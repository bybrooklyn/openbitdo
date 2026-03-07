use crate::app::state::{AppState, DashboardLayoutMode, PanelFocus};
use crate::ui::layout::{inner_rect, panel_block, truncate_to_width, HitMap, HitRegion, HitTarget};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::Style;
use ratatui::text::{Line, Span};
use ratatui::widgets::{List, ListItem, Paragraph};
use ratatui::Frame;

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let mut map = HitMap::default();

    let root = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Min(10), Constraint::Length(4)])
        .split(area);

    let status_hint = match state.dashboard_layout_mode {
        DashboardLayoutMode::Compact => "compact layout • resize for full three-panel view",
        DashboardLayoutMode::Wide => "click • arrows • Enter • Esc/q",
    };
    let selected_summary = state
        .selected_device()
        .map(|device| device.name.clone())
        .unwrap_or_else(|| "No controller selected".to_owned());
    let status = Paragraph::new(vec![
        Line::from(Span::raw(state.status_line.clone())),
        Line::from(vec![
            Span::styled(selected_summary, crate::ui::theme::subtle_style()),
            Span::raw("  •  "),
            Span::styled(status_hint, crate::ui::theme::subtle_style()),
        ]),
    ])
    .block(panel_block("Status", None, false));
    frame.render_widget(status, root[1]);

    match state.dashboard_layout_mode {
        DashboardLayoutMode::Wide => render_wide(frame, state, root[0], &mut map),
        DashboardLayoutMode::Compact => render_compact(frame, state, root[0], &mut map),
    }

    map
}

fn render_wide(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let columns = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(40),
            Constraint::Percentage(30),
            Constraint::Percentage(30),
        ])
        .split(area);

    render_devices(frame, state, columns[0], map);
    render_selected_device(frame, state, columns[1]);
    render_sidebar(frame, state, columns[2], map);
}

fn render_compact(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(9), Constraint::Min(6)])
        .split(area);

    let top = if area.width < 60 {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
            .split(rows[0])
    } else {
        Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
            .split(rows[0])
    };

    render_devices(frame, state, top[0], map);
    render_selected_device(frame, state, top[1]);
    render_sidebar(frame, state, rows[1], map);
}

fn render_devices(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(3), Constraint::Min(5)])
        .split(area);

    let filter_label = if state.last_panel_focus == PanelFocus::Devices {
        format!("{} active", state.device_filter)
    } else {
        state.device_filter.clone()
    };

    let filter = Paragraph::new(Line::from(vec![
        Span::styled("Search ", crate::ui::theme::title_style()),
        Span::raw(if filter_label.is_empty() {
            "type a model, VID, or PID".to_owned()
        } else {
            filter_label
        }),
    ]))
    .block(panel_block(
        "Search",
        Some("filter"),
        state.last_panel_focus == PanelFocus::Devices,
    ));
    frame.render_widget(filter, chunks[0]);
    map.push(chunks[0], HitTarget::FilterInput);

    let filtered = state.filtered_device_indices();
    let mut rows = Vec::new();
    if filtered.is_empty() {
        rows.push(ListItem::new(vec![
            Line::from("No matching devices"),
            Line::from(Span::styled(
                "Try a broader search or refresh the device list",
                crate::ui::theme::subtle_style(),
            )),
        ]));
    } else {
        for (display_idx, device_idx) in filtered.iter().copied().enumerate() {
            let dev = &state.devices[device_idx];
            let selected = state
                .selected_device_id
                .map(|id| id == dev.vid_pid)
                .unwrap_or(false)
                || display_idx == state.selected_filtered_index;
            let prefix = if selected { "›" } else { " " };
            let title = format!(
                "{prefix} {:04x}:{:04x} {}",
                dev.vid_pid.vid,
                dev.vid_pid.pid,
                truncate_to_width(&dev.name, chunks[1].width.saturating_sub(18) as usize)
            );
            let detail = format!(
                "{} • {} • {}",
                support_tier_short(dev.support_tier),
                protocol_short(dev.protocol_family),
                evidence_short(dev.evidence)
            );
            let style = if selected {
                crate::ui::theme::selected_row_style()
            } else {
                Style::default()
            };
            rows.push(
                ListItem::new(vec![
                    Line::from(Span::styled(title, style)),
                    Line::from(Span::styled(detail, crate::ui::theme::subtle_style())),
                ])
                .style(style),
            );
        }
    }

    let list = List::new(rows).block(panel_block("Controllers", Some("detected"), true));
    frame.render_widget(list, chunks[1]);

    map.extend(device_regions(
        chunks[1],
        state.filtered_device_indices().len(),
    ));
}

fn render_selected_device(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let selected = state.selected_device();
    let lines = if let Some(device) = selected {
        let mut details = vec![
            Line::from(vec![
                Span::styled(device.name.clone(), crate::ui::theme::screen_title_style()),
                Span::raw("  "),
                Span::styled(
                    format!("{:04x}:{:04x}", device.vid_pid.vid, device.vid_pid.pid),
                    crate::ui::theme::subtle_style(),
                ),
            ]),
            Line::from(format!(
                "Support: {}",
                support_tier_label(device.support_tier)
            )),
            Line::from(format!("Protocol: {:?}", device.protocol_family)),
            Line::from(format!("Evidence: {:?}", device.evidence)),
            Line::from(""),
            Line::from("Capabilities"),
        ];

        for capability in capability_lines(device) {
            details.push(Line::from(Span::styled(
                capability,
                crate::ui::theme::subtle_style(),
            )));
        }

        if device.support_tier != bitdo_proto::SupportTier::Full {
            details.push(Line::from(""));
            details.push(Line::from(Span::styled(
                "Write actions stay blocked until hardware confirmation lands.",
                crate::ui::theme::warning_style(),
            )));
        }

        details
    } else {
        vec![
            Line::from("No controller selected"),
            Line::from(""),
            Line::from(Span::styled(
                "Refresh the dashboard or connect a device to continue.",
                crate::ui::theme::subtle_style(),
            )),
        ]
    };

    let subtitle = selected
        .map(|device| support_tier_short(device.support_tier))
        .unwrap_or("idle");
    let panel = Paragraph::new(lines).block(panel_block("Device", Some(subtitle), true));
    frame.render_widget(panel, area);
}

fn render_sidebar(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(9), Constraint::Min(5)])
        .split(area);

    render_actions(frame, state, rows[0], map);
    render_events(frame, state, rows[1]);
}

fn render_actions(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let lines = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, quick)| {
            let prefix = if idx == state.selected_action_index {
                "›"
            } else {
                " "
            };
            let caption = quick
                .reason
                .as_deref()
                .map(compact_reason)
                .unwrap_or_else(|| action_caption(quick.action).to_owned());
            let style = if idx == state.selected_action_index {
                crate::ui::theme::selected_row_style()
            } else if quick.enabled {
                Style::default()
            } else {
                crate::ui::theme::muted_style()
            };

            ListItem::new(Line::from(vec![
                Span::styled(format!("{prefix} {}", quick.action.label()), style),
                Span::raw("  •  "),
                Span::styled(caption, crate::ui::theme::subtle_style()),
            ]))
        })
        .collect::<Vec<_>>();

    let panel = List::new(lines).block(panel_block("Actions", Some("Enter/click"), true));
    frame.render_widget(panel, area);

    let inner = inner_rect(area, 1, 1);
    let visible = inner.height as usize;
    for idx in 0..state.quick_actions.len().min(visible) {
        map.push(
            Rect::new(inner.x, inner.y + idx as u16, inner.width, 1),
            HitTarget::QuickAction(state.quick_actions[idx].action),
        );
    }
}

fn render_events(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let visible = area.height.saturating_sub(2) as usize;
    let entries = state
        .event_log
        .iter()
        .rev()
        .take(visible)
        .rev()
        .map(|entry| {
            let prefix = entry.timestamp_utc.to_string();
            let color = crate::ui::theme::level_color(entry.level);
            let level = match entry.level {
                crate::app::state::EventLevel::Info => "info",
                crate::app::state::EventLevel::Warning => "warn",
                crate::app::state::EventLevel::Error => "error",
            };
            ListItem::new(Line::from(vec![
                Span::styled(prefix, Style::default().fg(color)),
                Span::raw(" "),
                Span::styled(format!("[{level}]"), Style::default().fg(color)),
                Span::raw(" "),
                Span::raw(truncate_to_width(
                    &entry.message,
                    area.width.saturating_sub(20) as usize,
                )),
            ]))
        })
        .collect::<Vec<_>>();

    let widget = List::new(entries).block(panel_block("Activity", Some("events"), true));
    frame.render_widget(widget, area);
}

fn device_regions(list_rect: Rect, total_rows: usize) -> Vec<HitRegion> {
    let visible_rows = list_rect.height.saturating_sub(2) as usize / 2;
    let max = total_rows.min(visible_rows);
    let mut out = Vec::with_capacity(max);
    for idx in 0..max {
        let rect = Rect::new(
            list_rect.x.saturating_add(1),
            list_rect
                .y
                .saturating_add(1 + (idx as u16).saturating_mul(2)),
            list_rect.width.saturating_sub(2),
            2,
        );
        out.push(HitRegion {
            rect,
            target: HitTarget::DeviceRow(idx),
        });
    }
    out
}

fn truncate_reason(reason: &str) -> String {
    truncate_to_width(reason, 24)
}

fn action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::Refresh => "scan",
        crate::app::action::QuickAction::Diagnose => "probe",
        crate::app::action::QuickAction::RecommendedUpdate => "safe update",
        crate::app::action::QuickAction::EditMappings => "mapping",
        crate::app::action::QuickAction::Settings => "prefs",
        crate::app::action::QuickAction::Quit => "exit",
        _ => "available",
    }
}

fn support_tier_label(tier: bitdo_proto::SupportTier) -> &'static str {
    match tier {
        bitdo_proto::SupportTier::Full => "supported",
        bitdo_proto::SupportTier::CandidateReadOnly => "read-only",
        bitdo_proto::SupportTier::DetectOnly => "detect-only",
    }
}

fn support_tier_short(tier: bitdo_proto::SupportTier) -> &'static str {
    match tier {
        bitdo_proto::SupportTier::Full => "full",
        bitdo_proto::SupportTier::CandidateReadOnly => "ro",
        bitdo_proto::SupportTier::DetectOnly => "detect",
    }
}

fn capability_lines(device: &bitdo_app_core::AppDevice) -> Vec<String> {
    let mut lines = Vec::new();

    if device.capability.supports_firmware {
        lines.push("• firmware".to_owned());
    }
    if device.capability.supports_profile_rw {
        lines.push("• profile rw".to_owned());
    }
    if device.capability.supports_mode {
        lines.push("• mode switch".to_owned());
    }
    if device.capability.supports_jp108_dedicated_map {
        lines.push("• JP108 mapping".to_owned());
    }
    if device.capability.supports_u2_button_map || device.capability.supports_u2_slot_config {
        lines.push("• U2 slot + map".to_owned());
    }
    if lines.is_empty() {
        lines.push("• detect only".to_owned());
    }

    lines
}

fn compact_reason(reason: &str) -> String {
    if reason.contains("Read-only") {
        "read-only".to_owned()
    } else if reason.contains("restart") {
        "restart required".to_owned()
    } else {
        truncate_reason(reason)
    }
}

fn protocol_short(protocol: bitdo_proto::ProtocolFamily) -> &'static str {
    match protocol {
        bitdo_proto::ProtocolFamily::Standard64 => "S64",
        bitdo_proto::ProtocolFamily::Unknown => "unknown",
        _ => "other",
    }
}

fn evidence_short(evidence: bitdo_proto::SupportEvidence) -> &'static str {
    match evidence {
        bitdo_proto::SupportEvidence::Confirmed => "conf",
        bitdo_proto::SupportEvidence::Inferred => "infer",
        bitdo_proto::SupportEvidence::Untested => "untest",
    }
}
