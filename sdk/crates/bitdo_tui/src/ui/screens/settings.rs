use crate::app::state::AppState;
use crate::ui::layout::{
    action_grid_height, inner_rect, panel_block, render_action_strip, ActionDescriptor, HitMap,
    HitTarget,
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
            Constraint::Length(5),
            Constraint::Length(5),
            Constraint::Min(5),
            Constraint::Length(action_height),
            Constraint::Min(1),
        ])
        .split(area);

    let adv = Paragraph::new(vec![
        Line::from(Span::styled(
            if state.advanced_mode {
                "Advanced controls are on"
            } else {
                "Advanced controls are off"
            },
            crate::ui::theme::screen_title_style(),
        )),
        Line::from(Span::styled(
            "Turn this on only if you want expert labels and extra workflow options.",
            crate::ui::theme::subtle_style(),
        )),
    ])
    .block(panel_block("Advanced", Some("toggle"), true));
    frame.render_widget(adv, rows[0]);

    let report = Paragraph::new(vec![
        Line::from(Span::styled(
            format!("Support reports: {}", state.report_save_mode.as_str()),
            crate::ui::theme::screen_title_style(),
        )),
        Line::from(Span::styled(
            "Choose whether support reports save automatically after diagnostics or firmware work.",
            crate::ui::theme::subtle_style(),
        )),
    ])
    .block(panel_block("Reports", Some("save policy"), true));
    frame.render_widget(report, rows[1]);

    let settings_path = state
        .settings_path
        .as_ref()
        .map(|path| path.display().to_string())
        .unwrap_or_else(|| "not configured".to_owned());
    let status = Paragraph::new(vec![
        Line::from(state.status_line.as_str()),
        Line::from(""),
        Line::from(format!("Config path: {settings_path}")),
        Line::from(Span::styled(
            "Dashboard layout, filters, and report preferences persist when this path is available.",
            crate::ui::theme::subtle_style(),
        )),
    ])
    .block(panel_block("Status", Some("persistence"), true));
    frame.render_widget(status, rows[2]);

    let actions = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, action)| ActionDescriptor {
            action: action.action,
            label: action.action.label().to_owned(),
            caption: settings_action_caption(action.action).to_owned(),
            enabled: action.enabled,
            active: idx == state.selected_action_index,
        })
        .collect::<Vec<_>>();

    let mut map = HitMap::default();
    map.push(inner_click_rect(rows[0]), HitTarget::ToggleAdvancedMode);
    map.push(inner_click_rect(rows[1]), HitTarget::CycleReportMode);
    map.extend(render_action_strip(frame, rows[3], &actions));
    map
}

fn inner_click_rect(rect: Rect) -> Rect {
    inner_rect(rect, 1, 1)
}

fn settings_action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::Back => "return to dashboard",
        crate::app::action::QuickAction::Quit => "close OpenBitdo",
        _ => "available",
    }
}
