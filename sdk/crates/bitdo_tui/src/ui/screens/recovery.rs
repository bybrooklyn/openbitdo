use crate::app::state::AppState;
use crate::ui::layout::{
    action_grid_height, panel_block, render_action_strip, ActionDescriptor, HitMap,
};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;
use ratatui::Frame;

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let action_height = action_grid_height(area.width, state.quick_actions.len()).max(4);
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(6),
            Constraint::Min(6),
            Constraint::Length(action_height),
        ])
        .split(area);

    let body = Paragraph::new(vec![
        Line::from(Span::styled(
            "Recovery lock is active",
            crate::ui::theme::danger_style(),
        )),
        Line::from(""),
        Line::from("Write operations stay blocked until the app restarts."),
        Line::from("Restore a backup if one exists, validate the device, then restart."),
    ])
    .block(panel_block("Recovery", Some("safe rollback path"), true));
    frame.render_widget(body, rows[0]);

    let backup_line = if state.latest_backup.is_some() {
        "Backup detected. Restore Backup is available."
    } else {
        "No backup is registered for this session."
    };
    let detail = Paragraph::new(vec![
        Line::from("1. Restore backup if available."),
        Line::from("2. Confirm the controller responds normally."),
        Line::from("3. Restart OpenBitDo before any further writes."),
        Line::from(""),
        Line::from(Span::styled(backup_line, crate::ui::theme::subtle_style())),
        Line::from(Span::styled(
            state.status_line.clone(),
            crate::ui::theme::subtle_style(),
        )),
    ])
    .block(panel_block("Guidance", Some("recommended sequence"), true));
    frame.render_widget(detail, rows[1]);

    let actions = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, action)| ActionDescriptor {
            action: action.action,
            label: action.action.label().to_owned(),
            caption: recovery_action_caption(action.action).to_owned(),
            enabled: action.enabled,
            active: idx == state.selected_action_index,
        })
        .collect::<Vec<_>>();
    let mut map = HitMap::default();
    map.extend(render_action_strip(frame, rows[2], &actions));
    map
}

fn recovery_action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::RestoreBackup => "attempt rollback",
        crate::app::action::QuickAction::Back => "return to dashboard",
        crate::app::action::QuickAction::Quit => "exit openbitdo",
        _ => "available",
    }
}
