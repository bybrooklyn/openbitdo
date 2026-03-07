use crate::app::action::QuickAction;
use crate::app::state::{AppState, DiagnosticsFilter};
use crate::ui::layout::{
    action_grid_height, inner_rect, panel_block, render_action_strip, truncate_to_width,
    ActionDescriptor, HitMap, HitTarget,
};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{List, ListItem, Paragraph};
use ratatui::Frame;

pub fn render(frame: &mut Frame<'_>, state: &AppState, area: Rect) -> HitMap {
    let action_height = action_grid_height(area.width, state.quick_actions.len()).max(4);
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(5),
            Constraint::Min(9),
            Constraint::Length(action_height),
        ])
        .split(area);

    render_summary(frame, state, rows[0]);

    let body = if rows[1].width >= 92 {
        Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(44), Constraint::Percentage(56)])
            .split(rows[1])
    } else {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([Constraint::Percentage(40), Constraint::Percentage(60)])
            .split(rows[1])
    };

    let mut map = HitMap::default();
    render_check_panel(frame, state, body[0], &mut map);

    let detail_sections = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Percentage(58), Constraint::Percentage(42)])
        .split(body[1]);
    render_selected_check(frame, state, detail_sections[0]);
    render_next_steps(frame, state, detail_sections[1]);

    let actions = state
        .quick_actions
        .iter()
        .enumerate()
        .map(|(idx, action)| ActionDescriptor {
            action: action.action,
            label: action.action.label().to_owned(),
            caption: diagnostics_action_caption(action.action).to_owned(),
            enabled: action.enabled,
            active: idx == state.selected_action_index,
        })
        .collect::<Vec<_>>();
    map.extend(render_action_strip(frame, rows[2], &actions));
    map
}

fn render_summary(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let Some(diagnostics) = state.diagnostics_state.as_ref() else {
        let empty = Paragraph::new("No diagnostics result loaded.").block(panel_block(
            "Diagnostics",
            Some("summary"),
            true,
        ));
        frame.render_widget(empty, area);
        return;
    };

    let total = diagnostics.result.command_checks.len();
    let passed = diagnostics
        .result
        .command_checks
        .iter()
        .filter(|check| check.ok)
        .count();
    let issues = diagnostics
        .result
        .command_checks
        .iter()
        .filter(|check| !check.ok || check.severity != bitdo_proto::DiagSeverity::Ok)
        .count();
    let experimental = diagnostics
        .result
        .command_checks
        .iter()
        .filter(|check| check.is_experimental)
        .count();

    let transport = if diagnostics.result.transport_ready {
        "ready"
    } else {
        "degraded"
    };

    let lines = vec![
        Line::from(vec![
            Span::styled(
                format!("{passed}/{total} passed"),
                crate::ui::theme::screen_title_style(),
            ),
            Span::raw("  •  "),
            Span::styled(format!("{issues} issues"), severity_style(issues > 0)),
            Span::raw("  •  "),
            Span::styled(
                format!("{experimental} experimental"),
                crate::ui::theme::subtle_style(),
            ),
        ]),
        Line::from(vec![
            Span::styled(
                format!(
                    "Tier: {}",
                    support_tier_label(diagnostics.result.support_tier)
                ),
                crate::ui::theme::subtle_style(),
            ),
            Span::raw("  •  "),
            Span::styled(
                format!("Family: {:?}", diagnostics.result.protocol_family),
                crate::ui::theme::subtle_style(),
            ),
            Span::raw("  •  "),
            Span::styled(
                format!("Transport: {transport}"),
                crate::ui::theme::subtle_style(),
            ),
        ]),
    ];

    let panel = Paragraph::new(lines).block(panel_block("Diagnostics", Some("summary"), true));
    frame.render_widget(panel, area);
}

fn render_check_panel(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    if area.height < 8 {
        render_compact_check_panel(frame, state, area, map);
        return;
    }

    let sections = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(3), Constraint::Min(5)])
        .split(area);
    render_filter_row(frame, state, sections[0], map);

    let filtered = state.diagnostics_filtered_indices();
    let items = filtered
        .iter()
        .map(|check_index| {
            let check = &state
                .diagnostics_state
                .as_ref()
                .expect("diagnostics state present")
                .result
                .command_checks[*check_index];
            let selected = state
                .diagnostics_state
                .as_ref()
                .map(|diagnostics| diagnostics.selected_check_index == *check_index)
                .unwrap_or(false);
            let marker = if selected { "›" } else { " " };
            let experimental = if check.is_experimental { " exp" } else { "" };
            let line = format!(
                "{marker} {} {:?}{experimental}  {}",
                severity_badge(check.severity),
                check.command,
                truncate_to_width(&check.detail, sections[1].width.saturating_sub(26) as usize)
            );
            let style = if selected {
                crate::ui::theme::selected_row_style()
            } else {
                severity_row_style(check.severity)
            };
            ListItem::new(line).style(style)
        })
        .collect::<Vec<_>>();

    let list = if items.is_empty() {
        List::new(vec![ListItem::new("No checks in this filter")])
    } else {
        List::new(items)
    }
    .block(panel_block("Checks", Some("click a row"), true));
    frame.render_widget(list, sections[1]);

    let list_inner = inner_rect(sections[1], 1, 1);
    let visible_rows = list_inner.height as usize;
    for filtered_index in 0..filtered.len().min(visible_rows) {
        map.push(
            Rect::new(
                list_inner.x,
                list_inner.y + filtered_index as u16,
                list_inner.width,
                1,
            ),
            HitTarget::DiagnosticsCheck(filtered_index),
        );
    }
}

fn render_compact_check_panel(
    frame: &mut Frame<'_>,
    state: &AppState,
    area: Rect,
    map: &mut HitMap,
) {
    let diagnostics = state
        .diagnostics_state
        .as_ref()
        .expect("diagnostics state present");
    let filtered = state.diagnostics_filtered_indices();
    let inner = inner_rect(area, 1, 1);

    let filter_segments = compact_filter_segments(diagnostics);
    let filter_line = Line::from(
        filter_segments
            .iter()
            .enumerate()
            .map(|(idx, (filter, label))| {
                let prefix = if idx == 0 { "" } else { "  " };
                Span::styled(
                    format!("{prefix}{label}"),
                    if diagnostics.active_filter == *filter {
                        crate::ui::theme::screen_title_style()
                    } else {
                        crate::ui::theme::subtle_style()
                    },
                )
            })
            .collect::<Vec<_>>(),
    );

    let mut lines = vec![filter_line];
    let visible_rows = inner.height.saturating_sub(1) as usize;
    if filtered.is_empty() {
        lines.push(Line::from("No checks in this filter"));
    } else {
        for check_index in filtered.iter().take(visible_rows) {
            let check = &diagnostics.result.command_checks[*check_index];
            let marker = if diagnostics.selected_check_index == *check_index {
                "›"
            } else {
                " "
            };
            let experimental = if check.is_experimental { " exp" } else { "" };
            lines.push(Line::from(Span::styled(
                format!(
                    "{marker} {} {:?}{experimental}  {}",
                    severity_badge(check.severity),
                    check.command,
                    truncate_to_width(&check.detail, inner.width.saturating_sub(24) as usize,)
                ),
                if diagnostics.selected_check_index == *check_index {
                    crate::ui::theme::selected_row_style()
                } else {
                    severity_row_style(check.severity)
                },
            )));
        }
    }

    let panel = Paragraph::new(lines).block(panel_block("Checks", Some("tab cycles filter"), true));
    frame.render_widget(panel, area);

    let mut x = inner.x;
    for (idx, (filter, label)) in filter_segments.iter().enumerate() {
        let text = if idx == 0 {
            label.clone()
        } else {
            format!("  {label}")
        };
        let width = (text.len() as u16).min(inner.width.saturating_sub(x.saturating_sub(inner.x)));
        if width == 0 {
            break;
        }
        map.push(
            Rect::new(x, inner.y, width, 1),
            HitTarget::DiagnosticsFilter(*filter),
        );
        x = x.saturating_add(text.len() as u16);
    }

    for (row, _) in filtered.iter().take(visible_rows).enumerate() {
        map.push(
            Rect::new(
                inner.x,
                inner.y.saturating_add(row as u16).saturating_add(1),
                inner.width,
                1,
            ),
            HitTarget::DiagnosticsCheck(row),
        );
    }
}

fn render_filter_row(frame: &mut Frame<'_>, state: &AppState, area: Rect, map: &mut HitMap) {
    let diagnostics = state
        .diagnostics_state
        .as_ref()
        .expect("diagnostics state present");
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(34),
            Constraint::Percentage(33),
            Constraint::Percentage(33),
        ])
        .split(area);

    for (filter, rect) in DiagnosticsFilter::ALL
        .into_iter()
        .zip(chunks.iter().copied())
    {
        let active = diagnostics.active_filter == filter;
        let count = diagnostics
            .result
            .command_checks
            .iter()
            .filter(|check| filter.matches(check))
            .count();
        let chip = Paragraph::new(vec![
            Line::from(Span::styled(
                filter.label(),
                if active {
                    crate::ui::theme::screen_title_style()
                } else {
                    Style::default()
                },
            )),
            Line::from(Span::styled(
                format!("{count} checks"),
                crate::ui::theme::subtle_style(),
            )),
        ])
        .block(panel_block("Filter", Some(filter.label()), active));
        frame.render_widget(chip, rect);
        map.push(inner_rect(rect, 1, 1), HitTarget::DiagnosticsFilter(filter));
    }
}

fn render_selected_check(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let lines = if let Some(check) = state.selected_diagnostics_check() {
        let mut lines = vec![
            Line::from(vec![
                Span::styled(
                    format!("{:?}", check.command),
                    crate::ui::theme::screen_title_style(),
                ),
                Span::raw("  "),
                Span::styled(
                    severity_badge(check.severity),
                    severity_row_style(check.severity),
                ),
            ]),
            Line::from(format!("Severity: {:?}", check.severity)),
            Line::from(format!("Confidence: {:?}", check.confidence)),
            Line::from(format!(
                "Experimental: {}",
                if check.is_experimental { "yes" } else { "no" }
            )),
            Line::from(format!(
                "Error code: {}",
                check
                    .error_code
                    .map(|code| format!("{code:?}"))
                    .unwrap_or_else(|| "none".to_owned())
            )),
            Line::from(format!(
                "Response: {:?}  •  attempts {}",
                check.response_status, check.attempts
            )),
            Line::from(format!(
                "IO: wrote {}B, read {}B",
                check.bytes_written, check.bytes_read
            )),
            Line::from(format!(
                "Validator: {}",
                truncate_to_width(&check.validator, area.width.saturating_sub(13) as usize)
            )),
            Line::from(""),
            Line::from(Span::styled("Detail", crate::ui::theme::title_style())),
            Line::from(check.detail.clone()),
        ];

        if !check.parsed_facts.is_empty() {
            lines.push(Line::from(""));
            lines.push(Line::from(Span::styled(
                "Parsed facts",
                crate::ui::theme::title_style(),
            )));
            for (key, value) in &check.parsed_facts {
                lines.push(Line::from(format!("{key}: {value}")));
            }
        }
        lines
    } else {
        vec![
            Line::from("No diagnostics check selected."),
            Line::from(""),
            Line::from("Change filters or run diagnostics again."),
        ]
    };

    let detail = Paragraph::new(lines).block(panel_block("Selected Check", Some("detail"), true));
    frame.render_widget(detail, area);
}

fn render_next_steps(frame: &mut Frame<'_>, state: &AppState, area: Rect) {
    let Some(diagnostics) = state.diagnostics_state.as_ref() else {
        let empty = Paragraph::new("No diagnostics guidance available.").block(panel_block(
            "Next Steps",
            Some("guidance"),
            true,
        ));
        frame.render_widget(empty, area);
        return;
    };

    let report_line = diagnostics
        .latest_report_path
        .as_ref()
        .map(|path| format!("Saved report: {}", path.display()))
        .unwrap_or_else(|| "Saved report: not yet saved in this screen".to_owned());

    let content_width = area.width.saturating_sub(4) as usize;
    let action_line = format!(
        "Action: {}",
        truncate_to_width(
            recommended_next_action(diagnostics),
            content_width.saturating_sub(8)
        )
    );
    let summary_line = format!(
        "Summary: {}",
        truncate_to_width(&diagnostics.summary, content_width.saturating_sub(9))
    );
    let report_line = truncate_to_width(&report_line, content_width);
    let inner_height = area.height.saturating_sub(2);

    let lines = match inner_height {
        0 => Vec::new(),
        1 => vec![Line::from(if diagnostics.latest_report_path.is_some() {
            report_line.clone()
        } else {
            action_line.clone()
        })],
        2 => vec![
            Line::from(action_line),
            Line::from(Span::styled(report_line, crate::ui::theme::subtle_style())),
        ],
        _ => vec![
            Line::from(action_line),
            Line::from(summary_line),
            Line::from(Span::styled(report_line, crate::ui::theme::subtle_style())),
        ],
    };

    let panel = Paragraph::new(lines).block(panel_block("Next Steps", Some("guidance"), true));
    frame.render_widget(panel, area);
}

fn diagnostics_action_caption(action: QuickAction) -> &'static str {
    match action {
        QuickAction::RunAgain => "rerun safe-read probe",
        QuickAction::SaveReport => "write support report",
        QuickAction::Back => "return to dashboard",
        _ => "available",
    }
}

fn recommended_next_action(diagnostics: &crate::app::state::DiagnosticsState) -> &'static str {
    match diagnostics.result.support_tier {
        bitdo_proto::SupportTier::Full => {
            "Return to the dashboard and choose Recommended Update or Edit Mapping if needed."
        }
        bitdo_proto::SupportTier::CandidateReadOnly => {
            "Save or share the report. Update and mapping remain blocked until confirmation lands."
        }
        bitdo_proto::SupportTier::DetectOnly => {
            "Diagnostics only. Do not attempt update or mapping for this device."
        }
    }
}

fn severity_badge(severity: bitdo_proto::DiagSeverity) -> &'static str {
    match severity {
        bitdo_proto::DiagSeverity::Ok => "OK",
        bitdo_proto::DiagSeverity::Warning => "WARN",
        bitdo_proto::DiagSeverity::NeedsAttention => "ATTN",
    }
}

fn severity_row_style(severity: bitdo_proto::DiagSeverity) -> Style {
    match severity {
        bitdo_proto::DiagSeverity::Ok => Style::default().fg(Color::White),
        bitdo_proto::DiagSeverity::Warning => crate::ui::theme::warning_style(),
        bitdo_proto::DiagSeverity::NeedsAttention => crate::ui::theme::danger_style(),
    }
}

fn severity_style(has_issues: bool) -> Style {
    if has_issues {
        crate::ui::theme::warning_style()
    } else {
        crate::ui::theme::positive_style()
    }
}

fn support_tier_label(tier: bitdo_proto::SupportTier) -> &'static str {
    match tier {
        bitdo_proto::SupportTier::Full => "full",
        bitdo_proto::SupportTier::CandidateReadOnly => "candidate-readonly",
        bitdo_proto::SupportTier::DetectOnly => "detect-only",
    }
}

fn compact_filter_segments(
    diagnostics: &crate::app::state::DiagnosticsState,
) -> Vec<(DiagnosticsFilter, String)> {
    DiagnosticsFilter::ALL
        .into_iter()
        .map(|filter| {
            let count = diagnostics
                .result
                .command_checks
                .iter()
                .filter(|check| filter.matches(check))
                .count();
            let label = match filter {
                DiagnosticsFilter::All => format!("All {count}"),
                DiagnosticsFilter::Issues => format!("Issues {count}"),
                DiagnosticsFilter::Experimental => format!("Exp {count}"),
            };
            (filter, label)
        })
        .collect()
}
