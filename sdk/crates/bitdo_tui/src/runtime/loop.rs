use super::effect_executor::execute_effect;
use crate::app::event::AppEvent;
use crate::app::reducer::reduce;
use crate::app::state::{AppState, EventLevel, Screen, TaskMode};
use crate::persistence::ui_state::load_ui_state;
use crate::support_report::prune_reports_on_startup;
use crate::ui::layout::{self, HitTarget};
use crate::UiLaunchOptions;
use anyhow::Result;
use bitdo_app_core::{FirmwareProgressEvent, OpenBitdoCore};
use crossterm::event::{self, Event as CEvent, KeyCode, MouseButton, MouseEvent, MouseEventKind};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;
use std::collections::VecDeque;
use std::io::Stdout;
use tokio::sync::broadcast;
use tokio::time::Duration;

pub async fn run_ui_loop(core: OpenBitdoCore, opts: UiLaunchOptions) -> Result<()> {
    let _ = prune_reports_on_startup().await;

    let mut state = AppState::new(&opts);
    if let Some(path) = state.settings_path.as_ref() {
        match load_ui_state(path) {
            Ok(persisted) => {
                state.device_filter = persisted.device_filter_text;
                state.dashboard_layout_mode = persisted.dashboard_layout_mode;
                state.last_panel_focus = persisted.last_panel_focus;
                state.advanced_mode = persisted.advanced_mode;
                state.report_save_mode = persisted.report_save_mode;
            }
            Err(err) => {
                state.set_status("Settings file invalid; using defaults");
                state.append_event(EventLevel::Warning, format!("Settings load failed: {err}"));
            }
        }
    }
    core.set_advanced_mode(state.advanced_mode);

    let mut terminal = init_terminal()?;
    let mut hit_map = layout::HitMap::default();
    let mut firmware_events: Option<(String, broadcast::Receiver<FirmwareProgressEvent>)> = None;

    process_event(&core, &mut state, AppEvent::Init).await;

    loop {
        if let Ok(size) = terminal.size() {
            state.set_layout_from_size(size.width, size.height);
        }

        ensure_firmware_subscription(&core, &state, &mut firmware_events).await?;
        poll_firmware_events(&mut state, &mut firmware_events).await;
        process_event(&core, &mut state, AppEvent::Tick).await;

        terminal.draw(|frame| {
            hit_map = layout::render(frame, &state);
        })?;

        if state.quit_requested {
            break;
        }

        if !event::poll(Duration::from_millis(80))? {
            continue;
        }

        match event::read()? {
            CEvent::Key(key) => {
                if let Some(app_event) = key_to_event(&state, key.code) {
                    process_event(&core, &mut state, app_event).await;
                }
            }
            CEvent::Mouse(mouse) => {
                if let Some(app_event) = mouse_to_event(&state, &hit_map, mouse) {
                    process_event(&core, &mut state, app_event).await;
                }
            }
            CEvent::Resize(width, height) => {
                state.set_layout_from_size(width, height);
            }
            _ => {}
        }
    }

    teardown_terminal(&mut terminal)?;
    Ok(())
}

async fn process_event(core: &OpenBitdoCore, state: &mut AppState, initial: AppEvent) {
    let mut queue = VecDeque::from([initial]);
    while let Some(event) = queue.pop_front() {
        let effects = reduce(state, event);
        for effect in effects {
            let emitted = execute_effect(core, state, effect).await;
            for next in emitted {
                queue.push_back(next);
            }
        }
    }
}

async fn ensure_firmware_subscription(
    core: &OpenBitdoCore,
    state: &AppState,
    receiver: &mut Option<(String, broadcast::Receiver<FirmwareProgressEvent>)>,
) -> Result<()> {
    let Some(task) = state.task_state.as_ref() else {
        *receiver = None;
        return Ok(());
    };

    if !matches!(task.mode, TaskMode::Updating) {
        *receiver = None;
        return Ok(());
    }

    let Some(plan) = task.plan.as_ref() else {
        *receiver = None;
        return Ok(());
    };

    if receiver
        .as_ref()
        .map(|(session_id, _)| session_id == &plan.session_id.0)
        .unwrap_or(false)
    {
        return Ok(());
    }

    *receiver = Some((
        plan.session_id.0.clone(),
        core.subscribe_events(&plan.session_id.0).await?,
    ));
    Ok(())
}

async fn poll_firmware_events(
    state: &mut AppState,
    receiver: &mut Option<(String, broadcast::Receiver<FirmwareProgressEvent>)>,
) {
    let Some((_, rx)) = receiver.as_mut() else {
        return;
    };

    let mut events = Vec::new();
    loop {
        match rx.try_recv() {
            Ok(evt) => events.push(evt),
            Err(broadcast::error::TryRecvError::Empty) => break,
            Err(broadcast::error::TryRecvError::Lagged(_)) => continue,
            Err(broadcast::error::TryRecvError::Closed) => {
                *receiver = None;
                break;
            }
        }
    }

    for evt in events {
        let _ = reduce(state, AppEvent::UpdateProgress(evt));
    }
}

fn key_to_event(state: &AppState, key: KeyCode) -> Option<AppEvent> {
    match key {
        KeyCode::Char('q') => Some(AppEvent::Quit),
        KeyCode::Esc => Some(AppEvent::Back),
        KeyCode::Enter => Some(AppEvent::ConfirmPrimary),
        KeyCode::Down => match state.screen {
            Screen::Dashboard => Some(AppEvent::SelectNextDevice),
            Screen::Diagnostics => Some(AppEvent::DiagnosticsSelectNextCheck),
            Screen::MappingEditor => Some(AppEvent::MappingMoveSelection(1)),
            _ => Some(AppEvent::SelectNextAction),
        },
        KeyCode::Up => match state.screen {
            Screen::Dashboard => Some(AppEvent::SelectPrevDevice),
            Screen::Diagnostics => Some(AppEvent::DiagnosticsSelectPrevCheck),
            Screen::MappingEditor => Some(AppEvent::MappingMoveSelection(-1)),
            _ => Some(AppEvent::SelectPrevAction),
        },
        KeyCode::Left => match state.screen {
            Screen::MappingEditor => Some(AppEvent::MappingAdjust(-1)),
            Screen::Diagnostics => Some(AppEvent::SelectPrevAction),
            _ => Some(AppEvent::SelectPrevAction),
        },
        KeyCode::Right => match state.screen {
            Screen::MappingEditor => Some(AppEvent::MappingAdjust(1)),
            Screen::Diagnostics => Some(AppEvent::SelectNextAction),
            _ => Some(AppEvent::SelectNextAction),
        },
        KeyCode::Tab => {
            if state.screen == Screen::Diagnostics {
                Some(AppEvent::DiagnosticsShiftFilter(1))
            } else {
                None
            }
        }
        KeyCode::BackTab => {
            if state.screen == Screen::Diagnostics {
                Some(AppEvent::DiagnosticsShiftFilter(-1))
            } else {
                None
            }
        }
        KeyCode::Backspace => {
            if state.screen == Screen::Dashboard {
                Some(AppEvent::DeviceFilterBackspace)
            } else {
                None
            }
        }
        KeyCode::Char('t') => {
            if state.screen == Screen::Settings {
                Some(AppEvent::ToggleAdvancedMode)
            } else {
                None
            }
        }
        KeyCode::Char('r') => {
            if state.screen == Screen::Settings {
                Some(AppEvent::CycleReportSaveMode)
            } else {
                None
            }
        }
        KeyCode::Char(ch) => {
            if state.screen == Screen::Dashboard && !ch.is_control() {
                Some(AppEvent::DeviceFilterInput(ch))
            } else {
                None
            }
        }
        _ => None,
    }
}

fn mouse_to_event(
    state: &AppState,
    hit_map: &layout::HitMap,
    mouse: MouseEvent,
) -> Option<AppEvent> {
    match mouse.kind {
        MouseEventKind::Down(MouseButton::Left) => match hit_map.hit(mouse.column, mouse.row) {
            Some(HitTarget::DeviceRow(idx)) => Some(AppEvent::SelectFilteredDevice(idx)),
            Some(HitTarget::QuickAction(action)) => Some(AppEvent::TriggerAction(action)),
            Some(HitTarget::FilterInput) => Some(AppEvent::SelectFilteredDevice(0)),
            Some(HitTarget::DiagnosticsCheck(idx)) => Some(AppEvent::DiagnosticsSelectCheck(idx)),
            Some(HitTarget::DiagnosticsFilter(filter)) => {
                Some(AppEvent::DiagnosticsSetFilter(filter))
            }
            Some(HitTarget::ToggleAdvancedMode) => Some(AppEvent::ToggleAdvancedMode),
            Some(HitTarget::CycleReportMode) => Some(AppEvent::CycleReportSaveMode),
            None => None,
        },
        MouseEventKind::ScrollDown => match state.screen {
            Screen::Diagnostics => Some(AppEvent::DiagnosticsSelectNextCheck),
            _ => Some(AppEvent::SelectNextDevice),
        },
        MouseEventKind::ScrollUp => match state.screen {
            Screen::Diagnostics => Some(AppEvent::DiagnosticsSelectPrevCheck),
            _ => Some(AppEvent::SelectPrevDevice),
        },
        _ => None,
    }
}

fn init_terminal() -> Result<Terminal<CrosstermBackend<Stdout>>> {
    use crossterm::event::EnableMouseCapture;

    enable_raw_mode()?;
    let mut stdout = std::io::stdout();
    execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let terminal = Terminal::new(backend)?;
    Ok(terminal)
}

fn teardown_terminal(terminal: &mut Terminal<CrosstermBackend<Stdout>>) -> Result<()> {
    use crossterm::event::DisableMouseCapture;

    disable_raw_mode()?;
    execute!(
        terminal.backend_mut(),
        LeaveAlternateScreen,
        DisableMouseCapture
    )?;
    terminal.show_cursor()?;
    Ok(())
}
