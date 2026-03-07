use crate::app::state::{AppState, TaskMode};
use crate::ui::layout::{
    action_grid_height, panel_block, render_action_strip, ActionDescriptor, HitMap,
};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::Style;
use ratatui::text::{Line, Span};
use ratatui::widgets::{Gauge, Paragraph};
use ratatui::Frame;

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let action_height = action_grid_height(area.width, state.quick_actions.len()).max(4);
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(6),
            Constraint::Min(8),
            Constraint::Length(action_height),
        ])
        .split(area);

    let task = state.task_state.as_ref();
    let title = match task.map(|t| t.mode) {
        Some(TaskMode::Diagnostics) => "Diagnostics",
        Some(TaskMode::Preflight) => "Safety Check",
        Some(TaskMode::Updating) => "Update In Progress",
        Some(TaskMode::Final) => "Result",
        None => "Task",
    };

    let summary_lines = if let Some(task) = task {
        vec![
            Line::from(vec![
                Span::styled(
                    format!("{title} Workflow"),
                    crate::ui::theme::screen_title_style(),
                ),
                Span::raw("  "),
                Span::styled(
                    task_mode_caption(task.mode),
                    crate::ui::theme::subtle_style(),
                ),
            ]),
            Line::from(""),
            Line::from(task.status.clone()),
        ]
    } else {
        vec![
            Line::from(Span::styled(
                "No active workflow",
                crate::ui::theme::screen_title_style(),
            )),
            Line::from(""),
            Line::from("Choose a controller action from the dashboard to begin."),
        ]
    };

    let summary =
        Paragraph::new(summary_lines).block(panel_block(title, Some("status and intent"), true));
    frame.render_widget(summary, rows[0]);

    render_task_details(frame, state, rows[1]);

    let mut map = HitMap::default();
    let action_rows = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, action)| ActionDescriptor {
            action: action.action,
            label: action.action.label().to_owned(),
            caption: task_action_caption(action.action).to_owned(),
            enabled: action.enabled,
            active: idx == state.selected_action_index,
        })
        .collect::<Vec<_>>();
    map.extend(render_action_strip(frame, rows[2], &action_rows));
    map
}

fn render_task_details(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let task = state.task_state.as_ref();
    let columns = if area.width >= 76 {
        Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(58), Constraint::Percentage(42)])
            .split(area)
    } else {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
            .split(area)
    };

    let detail_lines = if let Some(task) = task {
        let mut lines = vec![Line::from(task.status.clone())];
        if let Some(plan) = task.plan.as_ref() {
            lines.push(Line::from(""));
            lines.push(Line::from(format!(
                "Transfer session: {:?}",
                plan.session_id
            )));
            lines.push(Line::from(format!("Chunk size: {} bytes", plan.chunk_size)));
            lines.push(Line::from(format!("Total chunks: {}", plan.chunks_total)));
            lines.push(Line::from(format!(
                "Estimated transfer time: {}s",
                plan.expected_seconds
            )));
            if !plan.warnings.is_empty() {
                lines.push(Line::from(""));
                lines.push(Line::from(Span::styled(
                    "Safety notes",
                    crate::ui::theme::warning_style(),
                )));
                for warning in &plan.warnings {
                    lines.push(Line::from(format!("• {warning}")));
                }
            }
        }
        if let Some(final_report) = task.final_report.as_ref() {
            lines.push(Line::from(""));
            lines.push(Line::from(format!(
                "Final status: {:?}",
                final_report.status
            )));
            lines.push(Line::from(format!(
                "Transfer: {}/{} chunks",
                final_report.chunks_sent, final_report.chunks_total
            )));
            lines.push(Line::from(format!("Message: {}", final_report.message)));
        }
        lines
    } else {
        vec![Line::from("No details available")]
    };
    let detail =
        Paragraph::new(detail_lines).block(panel_block("Details", Some("workflow context"), true));
    frame.render_widget(detail, columns[0]);

    let progress = task.map(|task| task.progress).unwrap_or_default();
    let progress_rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(4), Constraint::Min(4)])
        .split(columns[1]);
    let gauge = Gauge::default()
        .block(panel_block("Progress", Some("transfer state"), true))
        .gauge_style(Style::default().fg(ratatui::style::Color::Green))
        .percent(progress as u16)
        .label(format!("{progress}%"));
    frame.render_widget(gauge, progress_rows[0]);

    let summary_lines = if let Some(task) = task {
        vec![
            Line::from(format!("Current stage: {}", task_mode_caption(task.mode))),
            Line::from(format!("Progress: {progress}%")),
            Line::from(format!(
                "Report policy: {}",
                state.report_save_mode.as_str()
            )),
            Line::from(Span::styled(
                state.status_line.clone(),
                crate::ui::theme::subtle_style(),
            )),
        ]
    } else {
        vec![Line::from("Choose an action to see its workflow details.")]
    };
    let summary =
        Paragraph::new(summary_lines).block(panel_block("Context", Some("current session"), true));
    frame.render_widget(summary, progress_rows[1]);
}

fn task_mode_caption(mode: TaskMode) -> &'static str {
    match mode {
        TaskMode::Diagnostics => "running safe diagnostics",
        TaskMode::Preflight => "reviewing update safety",
        TaskMode::Updating => "sending verified firmware",
        TaskMode::Final => "showing the final result",
    }
}

fn task_action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::Confirm => "acknowledge risk and start the update",
        crate::app::action::QuickAction::Cancel => "stop and discard this step",
        crate::app::action::QuickAction::Back => "leave this screen",
        _ => "available",
    }
}
