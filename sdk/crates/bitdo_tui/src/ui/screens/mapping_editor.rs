use crate::app::state::{AppState, MappingDraftState};
use crate::ui::layout::{
    action_grid_height, panel_block, render_action_strip, ActionDescriptor, HitMap,
};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{List, ListItem, Paragraph};
use ratatui::Frame;

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let action_height = action_grid_height(area.width, state.quick_actions.len()).max(4);
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(5),
            Constraint::Min(8),
            Constraint::Length(action_height),
        ])
        .split(area);

    let mut lines = vec![
        Line::from(Span::styled(
            "Apply is explicit. Arrow keys adjust only the highlighted mapping.",
            crate::ui::theme::subtle_style(),
        )),
        Line::from(""),
    ];
    let mut mapping_rows = Vec::new();
    let mut inspector_lines = Vec::new();

    match state.mapping_draft_state.as_ref() {
        Some(MappingDraftState::Jp108 {
            current,
            selected_row,
            ..
        }) => {
            lines.push(Line::from(Span::styled(
                "JP108 dedicated mapping",
                crate::ui::theme::screen_title_style(),
            )));
            for (idx, entry) in current.iter().enumerate() {
                let style = if idx == *selected_row {
                    Style::default().add_modifier(Modifier::BOLD)
                } else {
                    Style::default()
                };
                let marker = if idx == *selected_row { "›" } else { " " };
                mapping_rows.push(
                    ListItem::new(format!(
                        "{marker} {:?}  ->  0x{:04x}",
                        entry.button, entry.target_hid_usage
                    ))
                    .style(style),
                );
            }
            if let Some(selected) = current.get(*selected_row) {
                inspector_lines.push(Line::from(format!("Button: {:?}", selected.button)));
                inspector_lines.push(Line::from(format!(
                    "Target HID: 0x{:04x}",
                    selected.target_hid_usage
                )));
                inspector_lines.push(Line::from(""));
                inspector_lines.push(Line::from("Left/right cycles preset targets."));
            }
        }
        Some(MappingDraftState::Ultimate2 {
            current,
            selected_row,
            ..
        }) => {
            lines.push(Line::from(Span::styled(
                format!(
                    "Ultimate2 profile slot {:?} mode {}",
                    current.slot, current.mode
                ),
                crate::ui::theme::screen_title_style(),
            )));
            lines.push(Line::from(Span::styled(
                format!("L2 {:.2} | R2 {:.2}", current.l2_analog, current.r2_analog),
                crate::ui::theme::subtle_style(),
            )));
            for (idx, entry) in current.mappings.iter().enumerate() {
                let style = if idx == *selected_row {
                    Style::default().add_modifier(Modifier::BOLD)
                } else {
                    Style::default()
                };
                let marker = if idx == *selected_row { "›" } else { " " };
                mapping_rows.push(
                    ListItem::new(format!(
                        "{marker} {:?}  ->  {} (0x{:04x})",
                        entry.button,
                        u2_target_label(entry.target_hid_usage),
                        entry.target_hid_usage
                    ))
                    .style(style),
                );
            }
            if let Some(selected) = current.mappings.get(*selected_row) {
                inspector_lines.push(Line::from(format!("Button: {:?}", selected.button)));
                inspector_lines.push(Line::from(format!(
                    "Target: {} (0x{:04x})",
                    u2_target_label(selected.target_hid_usage),
                    selected.target_hid_usage
                )));
                inspector_lines.push(Line::from(""));
                inspector_lines.push(Line::from("Left/right cycles preset targets."));
            }
        }
        None => {
            lines.push(Line::from("No mapping draft loaded."));
            inspector_lines.push(Line::from(
                "Select Edit Mapping from the dashboard to begin.",
            ));
        }
    }

    let intro = Paragraph::new(lines).block(panel_block(
        "Mapping Studio",
        Some(if state.mapping_has_changes() {
            "modified"
        } else {
            "clean"
        }),
        true,
    ));
    frame.render_widget(intro, rows[0]);

    let body = if rows[1].width >= 80 {
        Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(62), Constraint::Percentage(38)])
            .split(rows[1])
    } else {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([Constraint::Percentage(62), Constraint::Percentage(38)])
            .split(rows[1])
    };

    let table = List::new(mapping_rows).block(panel_block(
        "Mappings",
        Some("up/down select • left/right adjust"),
        true,
    ));
    frame.render_widget(table, body[0]);

    let mut status_lines = inspector_lines;
    status_lines.push(Line::from(""));
    status_lines.push(Line::from(Span::styled(
        state.status_line.clone(),
        crate::ui::theme::subtle_style(),
    )));
    let status = Paragraph::new(status_lines).block(panel_block(
        "Inspector",
        Some("selected mapping"),
        true,
    ));
    frame.render_widget(status, body[1]);

    let actions = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, action)| ActionDescriptor {
            action: action.action,
            label: action.action.label().to_owned(),
            caption: mapping_action_caption(action.action).to_owned(),
            enabled: action.enabled,
            active: idx == state.selected_action_index,
        })
        .collect::<Vec<_>>();

    let mut map = HitMap::default();
    map.extend(render_action_strip(frame, rows[2], &actions));
    map
}

fn mapping_action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::ApplyDraft => "write current draft",
        crate::app::action::QuickAction::UndoDraft => "restore last edit",
        crate::app::action::QuickAction::ResetDraft => "discard draft changes",
        crate::app::action::QuickAction::RestoreBackup => "recover saved backup",
        crate::app::action::QuickAction::Firmware => "switch to firmware flow",
        crate::app::action::QuickAction::Back => "return to dashboard",
        _ => "available",
    }
}

fn u2_target_label(target: u16) -> &'static str {
    match target {
        0x0100 => "A",
        0x0101 => "B",
        0x0102 => "X",
        0x0103 => "Y",
        0x0104 => "L1",
        0x0105 => "R1",
        0x0106 => "L2",
        0x0107 => "R2",
        0x0108 => "L3",
        0x0109 => "R3",
        0x010a => "Select",
        0x010b => "Start",
        0x010c => "Home",
        0x010d => "DPadUp",
        0x010e => "DPadDown",
        0x010f => "DPadLeft",
        0x0110 => "DPadRight",
        _ => "Unknown",
    }
}
