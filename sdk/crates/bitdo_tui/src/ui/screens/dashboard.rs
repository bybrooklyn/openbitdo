use crate::app::state::{AppState, DashboardLayoutMode, PanelFocus};
use crate::ui::layout::{HitMap, HitTarget, inner_rect, panel_block, truncate_to_width};
use bitdo_app_core::SupportScorecard;
use bitdo_proto::SupportTier;
use ratatui::Frame;
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::Style;
use ratatui::text::{Line, Span};
use ratatui::widgets::{List, ListItem, Paragraph};

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let mut map = HitMap::default();

    let root = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Min(10), Constraint::Length(4)])
        .split(area);

    let status_hint = match state.dashboard_layout_mode {
        DashboardLayoutMode::Compact => {
            "compact layout • resize for three panels or keep using click, arrows, and Enter"
        }
        DashboardLayoutMode::Wide => {
            "click a device or action • arrows, Enter, Esc, and q still work"
        }
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
            "type a model name or USB ID".to_owned()
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
    let mut hit_rows = Vec::new();
    if state.devices.is_empty() {
        rows.push(ListItem::new(vec![
            Line::from("No 8BitDo controller detected"),
            Line::from(Span::styled(
                "Reconnect over USB/Bluetooth, check HID permissions, then Refresh",
                crate::ui::theme::subtle_style(),
            )),
            Line::from(Span::styled(
                "Use openbitdo --mock to preview the full workflow without hardware",
                crate::ui::theme::subtle_style(),
            )),
        ]));
    } else if filtered.is_empty() {
        rows.push(ListItem::new(vec![
            Line::from("No matching devices"),
            Line::from(Span::styled(
                "Try a broader search or refresh the device list",
                crate::ui::theme::subtle_style(),
            )),
        ]));
    } else {
        let mut last_tier = None;
        let mut visual_row = 0usize;
        let visible_rows = inner_rect(chunks[1], 1, 1).height as usize;
        for (display_idx, device_idx) in filtered.iter().copied().enumerate() {
            let dev = &state.devices[device_idx];
            if last_tier != Some(dev.support_tier) {
                last_tier = Some(dev.support_tier);
                rows.push(ListItem::new(Line::from(Span::styled(
                    group_header(dev.support_tier, group_count(state, dev.support_tier)),
                    crate::ui::theme::title_style(),
                ))));
                visual_row += 1;
            }
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
            if visual_row < visible_rows {
                hit_rows.push((display_idx, visual_row, visible_rows - visual_row));
            }
            visual_row += 2;
        }
    }

    let list = List::new(rows).block(panel_block(
        "Controllers",
        Some(if state.devices.is_empty() {
            "connect"
        } else {
            "grouped"
        }),
        true,
    ));
    frame.render_widget(list, chunks[1]);

    let inner = inner_rect(chunks[1], 1, 1);
    for (display_idx, row, remaining) in hit_rows {
        map.push(
            Rect::new(
                inner.x,
                inner.y + row as u16,
                inner.width,
                2.min(remaining) as u16,
            ),
            HitTarget::DeviceRow(display_idx),
        );
    }
}

fn render_selected_device(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let selected = state.selected_device();
    let lines = if let Some(device) = selected {
        let scorecard = device.scorecard();
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
                "Support: {}  •  Status: {}",
                support_tier_label(device.support_tier),
                device.support_status().as_str()
            )),
            Line::from(format!("Protocol: {:?}", device.protocol_family)),
            Line::from(format!("Evidence: {:?}", device.evidence)),
            Line::from(""),
            Line::from(Span::styled(
                "Support Scorecard",
                crate::ui::theme::title_style(),
            )),
            Line::from(Span::styled(
                format!(
                    "• {}% complete  •  promotion {}",
                    scorecard.score_percent,
                    if scorecard.promotion_ready {
                        "ready"
                    } else {
                        "blocked"
                    }
                ),
                crate::ui::theme::subtle_style(),
            )),
            Line::from(""),
            Line::from(Span::styled("Works Now", crate::ui::theme::title_style())),
        ];

        for capability in works_now_lines(device) {
            details.push(Line::from(Span::styled(
                capability,
                crate::ui::theme::subtle_style(),
            )));
        }

        details.push(Line::from(""));
        details.push(Line::from(Span::styled(
            "Blocked",
            crate::ui::theme::title_style(),
        )));
        for blocked in blocked_lines(device) {
            details.push(Line::from(Span::styled(
                blocked,
                if device.support_tier == SupportTier::Full {
                    crate::ui::theme::subtle_style()
                } else {
                    crate::ui::theme::warning_style()
                },
            )));
        }

        details.push(Line::from(""));
        details.push(Line::from(Span::styled(
            "Missing Evidence",
            crate::ui::theme::title_style(),
        )));
        for gap in scorecard_gap_lines(&scorecard) {
            details.push(Line::from(Span::styled(
                gap,
                crate::ui::theme::subtle_style(),
            )));
        }

        details
    } else {
        vec![
            Line::from(Span::styled(
                "No controller selected",
                crate::ui::theme::screen_title_style(),
            )),
            Line::from(""),
            Line::from(Span::styled(
                "Connect an 8BitDo controller, then choose Refresh.",
                crate::ui::theme::subtle_style(),
            )),
            Line::from(Span::styled(
                "If the OS blocks HID access, fix permissions and refresh again.",
                crate::ui::theme::subtle_style(),
            )),
            Line::from(Span::styled(
                "Use openbitdo --mock to preview supported, read-only, and detect-only flows.",
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

fn truncate_reason(reason: &str) -> String {
    truncate_to_width(reason, 24)
}

fn action_caption(action: crate::app::action::QuickAction) -> &'static str {
    match action {
        crate::app::action::QuickAction::Refresh => "scan USB/HID again",
        crate::app::action::QuickAction::Diagnose => "run safe reads and build support evidence",
        crate::app::action::QuickAction::RecommendedUpdate => {
            "verified firmware for confirmed devices"
        }
        crate::app::action::QuickAction::EditMappings => {
            "mapping only after read/write confirmation"
        }
        crate::app::action::QuickAction::UnlockWriteProbe => {
            "guarded candidate write/readback probe"
        }
        crate::app::action::QuickAction::Settings => "report saving and interface preferences",
        crate::app::action::QuickAction::Quit => "close OpenBitdo",
        _ => "available",
    }
}

fn support_tier_label(tier: bitdo_proto::SupportTier) -> &'static str {
    match tier {
        bitdo_proto::SupportTier::Full => "Supported",
        bitdo_proto::SupportTier::CandidateReadOnly => "Read-only candidate",
        bitdo_proto::SupportTier::DetectOnly => "Detection only",
    }
}

fn support_tier_short(tier: bitdo_proto::SupportTier) -> &'static str {
    match tier {
        bitdo_proto::SupportTier::Full => "supported",
        bitdo_proto::SupportTier::CandidateReadOnly => "read-only",
        bitdo_proto::SupportTier::DetectOnly => "detect-only",
    }
}

fn group_header(tier: SupportTier, count: usize) -> String {
    let label = match tier {
        SupportTier::Full => "Supported now",
        SupportTier::CandidateReadOnly => "Read-only candidates",
        SupportTier::DetectOnly => "Detect-only",
    };
    format!("{label} ({count})")
}

fn group_count(state: &AppState, tier: SupportTier) -> usize {
    state
        .filtered_device_indices()
        .into_iter()
        .filter(|idx| state.devices[*idx].support_tier == tier)
        .count()
}

fn works_now_lines(device: &bitdo_app_core::AppDevice) -> Vec<String> {
    let mut lines = Vec::new();

    lines.push("• safe diagnostics and support report".to_owned());
    lines.push("• device identification and support-state guidance".to_owned());

    if device.capability.supports_firmware {
        lines.push("• firmware updates".to_owned());
    }
    if device.capability.supports_profile_rw {
        lines.push("• profile read and write".to_owned());
    }
    if device.capability.supports_mode {
        lines.push("• mode switching".to_owned());
    }
    if device.capability.supports_jp108_dedicated_map {
        lines.push("• JP108 dedicated mapping".to_owned());
    }
    if device.capability.supports_u2_button_map || device.capability.supports_u2_slot_config {
        lines.push("• Ultimate 2 slot and mapping".to_owned());
    }

    lines
}

fn blocked_lines(device: &bitdo_app_core::AppDevice) -> Vec<String> {
    match device.support_tier {
        SupportTier::Full => {
            let mut lines = Vec::new();
            if !device.capability.supports_firmware {
                lines.push("• firmware update: no verified path for this PID".to_owned());
            }
            if !(device.capability.supports_jp108_dedicated_map
                || (device.capability.supports_u2_button_map
                    && device.capability.supports_u2_slot_config))
            {
                lines.push("• mapping editor: no confirmed mapping surface".to_owned());
            }
            if lines.is_empty() {
                lines.push("• none for confirmed capabilities".to_owned());
            }
            lines
        }
        SupportTier::CandidateReadOnly => vec![
            "• firmware writes blocked until runtime traces are confirmed".to_owned(),
            "• mapping/profile writes blocked until hardware read/write/readback passes".to_owned(),
        ],
        SupportTier::DetectOnly => vec![
            "• diagnostics beyond identification are limited".to_owned(),
            "• firmware, mapping, profile, and mode writes are not available".to_owned(),
        ],
    }
}

fn scorecard_gap_lines(scorecard: &SupportScorecard) -> Vec<String> {
    if scorecard.missing_evidence.is_empty() {
        return vec!["• no blocking evidence gaps for current support tier".to_owned()];
    }

    scorecard
        .missing_evidence
        .iter()
        .take(4)
        .map(|gap| format!("• {gap}"))
        .collect()
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
        bitdo_proto::ProtocolFamily::Standard64 => "standard64",
        bitdo_proto::ProtocolFamily::DInput => "dinput",
        bitdo_proto::ProtocolFamily::JpHandshake => "jp",
        bitdo_proto::ProtocolFamily::Unknown => "unknown",
        _ => "other",
    }
}

fn evidence_short(evidence: bitdo_proto::SupportEvidence) -> &'static str {
    match evidence {
        bitdo_proto::SupportEvidence::Confirmed => "confirmed",
        bitdo_proto::SupportEvidence::Inferred => "inferred",
        bitdo_proto::SupportEvidence::Untested => "untested",
    }
}
