use crate::app::action::QuickAction;
use crate::app::state::{AppState, DiagnosticsFilter, Screen};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Borders, Paragraph};
use ratatui::Frame;
use unicode_width::{UnicodeWidthChar, UnicodeWidthStr};

use super::screens::{dashboard, diagnostics, mapping_editor, recovery, settings, task};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HitTarget {
    DeviceRow(usize),
    QuickAction(QuickAction),
    FilterInput,
    DiagnosticsCheck(usize),
    DiagnosticsFilter(DiagnosticsFilter),
    ToggleAdvancedMode,
    CycleReportMode,
}

#[derive(Clone, Copy, Debug)]
pub struct HitRegion {
    pub rect: Rect,
    pub target: HitTarget,
}

#[derive(Clone, Debug, Default)]
pub struct HitMap {
    pub regions: Vec<HitRegion>,
}

#[derive(Clone, Debug)]
pub struct ActionDescriptor {
    pub action: QuickAction,
    pub label: String,
    pub caption: String,
    pub enabled: bool,
    pub active: bool,
}

impl HitMap {
    pub fn push(&mut self, rect: Rect, target: HitTarget) {
        self.regions.push(HitRegion { rect, target });
    }

    pub fn extend(&mut self, regions: Vec<HitRegion>) {
        self.regions.extend(regions);
    }

    pub fn hit(&self, x: u16, y: u16) -> Option<HitTarget> {
        self.regions
            .iter()
            .find(|region| point_in_rect(x, y, region.rect))
            .map(|region| region.target)
    }
}

pub fn render(frame: &mut Frame<'_>, state: &AppState) -> HitMap {
    let root = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Length(3), Constraint::Min(1)])
        .split(frame.area());

    render_header(frame, root[0], state);

    match state.screen {
        Screen::Dashboard => dashboard::render(frame, state, root[1]),
        Screen::Task => task::render(frame, state, root[1]),
        Screen::Diagnostics => diagnostics::render(frame, state, root[1]),
        Screen::MappingEditor => mapping_editor::render(frame, state, root[1]),
        Screen::Recovery => recovery::render(frame, state, root[1]),
        Screen::Settings => settings::render(frame, state, root[1]),
    }
}

pub fn render_action_strip(
    frame: &mut Frame<'_>,
    area: Rect,
    actions: &[ActionDescriptor],
) -> Vec<HitRegion> {
    if actions.is_empty() {
        return Vec::new();
    }

    let columns = action_columns(area.width, actions.len());
    let rows = actions.len().div_ceil(columns);
    let row_constraints = vec![Constraint::Length(4); rows];
    let row_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints(row_constraints)
        .split(area);

    let mut regions = Vec::new();

    for (row_idx, row_rect) in row_chunks.iter().copied().enumerate() {
        let start = row_idx * columns;
        let end = (start + columns).min(actions.len());
        let row_actions = &actions[start..end];
        let constraints = vec![
            Constraint::Percentage((100 / row_actions.len()).max(1) as u16);
            row_actions.len()
        ];
        let col_chunks = Layout::default()
            .direction(Direction::Horizontal)
            .constraints(constraints)
            .split(row_rect);

        for (descriptor, rect) in row_actions.iter().zip(col_chunks.iter().copied()) {
            let label_width = rect.width.saturating_sub(4) as usize;
            let style = crate::ui::theme::action_label_style(descriptor.active, descriptor.enabled);
            let border = crate::ui::theme::border_style(descriptor.active, descriptor.enabled);
            let label = truncate_to_width(&descriptor.label, label_width);
            let caption = truncate_to_width(&descriptor.caption, label_width);
            let body = Paragraph::new(vec![
                Line::from(Span::styled(label, style)),
                Line::from(Span::styled(
                    caption,
                    crate::ui::theme::action_caption_style(descriptor.enabled),
                )),
            ])
            .block(
                Block::default()
                    .borders(Borders::ALL)
                    .border_type(BorderType::Rounded)
                    .style(border)
                    .title(descriptor.action.label()),
            );
            frame.render_widget(body, rect);
            regions.push(HitRegion {
                rect,
                target: HitTarget::QuickAction(descriptor.action),
            });
        }
    }

    regions
}

pub fn action_grid_height(width: u16, count: usize) -> u16 {
    if count == 0 {
        0
    } else {
        let columns = action_columns(width, count);
        let rows = count.div_ceil(columns);
        (rows as u16).saturating_mul(4)
    }
}

pub fn panel_block<'a>(title: &'a str, subtitle: Option<&'a str>, active: bool) -> Block<'a> {
    let mut title_spans = vec![Span::styled(title, crate::ui::theme::title_style())];
    if let Some(subtitle) = subtitle.filter(|subtitle| !subtitle.is_empty()) {
        title_spans.push(Span::styled("  ", crate::ui::theme::subtle_style()));
        title_spans.push(Span::styled(subtitle, crate::ui::theme::subtle_style()));
    }

    Block::default()
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .style(crate::ui::theme::border_style(active, true))
        .title(Line::from(title_spans))
}

pub fn inner_rect(rect: Rect, horizontal: u16, vertical: u16) -> Rect {
    let x = rect.x.saturating_add(horizontal);
    let y = rect.y.saturating_add(vertical);
    let width = rect.width.saturating_sub(horizontal.saturating_mul(2));
    let height = rect.height.saturating_sub(vertical.saturating_mul(2));
    Rect::new(x, y, width, height)
}

fn render_header(frame: &mut Frame<'_>, area: Rect, state: &AppState) {
    let filtered = state.filtered_device_indices().len();
    let mode = if state.advanced_mode { "adv" } else { "safe" };
    let summary = match state.screen {
        Screen::Dashboard => format!("{filtered} devices"),
        Screen::Task => truncate_to_width(&state.status_line, 20),
        Screen::Diagnostics => state
            .diagnostics_state
            .as_ref()
            .map(|diagnostics| {
                let passed = diagnostics
                    .result
                    .command_checks
                    .iter()
                    .filter(|check| check.ok)
                    .count();
                format!(
                    "{passed}/{} passed",
                    diagnostics.result.command_checks.len()
                )
            })
            .unwrap_or_else(|| "diagnostics".to_owned()),
        Screen::MappingEditor => {
            if state.mapping_has_changes() {
                "draft modified".to_owned()
            } else {
                "draft clean".to_owned()
            }
        }
        Screen::Recovery => "write lock active".to_owned(),
        Screen::Settings => "preferences".to_owned(),
    };

    let line = Line::from(vec![
        Span::styled("OpenBitDo", crate::ui::theme::app_title_style()),
        Span::raw("  "),
        Span::styled(
            screen_label(state.screen),
            crate::ui::theme::screen_title_style(),
        ),
        Span::raw("  •  "),
        Span::styled(summary, crate::ui::theme::subtle_style()),
        Span::raw("  •  "),
        Span::styled(
            format!("reports {}", report_mode_short(state.report_save_mode)),
            crate::ui::theme::subtle_style(),
        ),
        Span::raw("  •  "),
        Span::styled(mode, crate::ui::theme::subtle_style()),
    ]);

    let header = Paragraph::new(line).block(panel_block("Session", None, true));
    frame.render_widget(header, area);
}

fn action_columns(width: u16, count: usize) -> usize {
    let desired = if width >= 110 {
        4
    } else if width >= 76 {
        3
    } else if width >= 48 {
        2
    } else {
        1
    };
    desired.min(count.max(1))
}

fn screen_label(screen: Screen) -> &'static str {
    match screen {
        Screen::Dashboard => "Dashboard",
        Screen::Task => "Workflow",
        Screen::Diagnostics => "Diagnostics",
        Screen::MappingEditor => "Mappings",
        Screen::Recovery => "Recovery",
        Screen::Settings => "Settings",
    }
}

fn report_mode_short(mode: crate::ReportSaveMode) -> &'static str {
    match mode {
        crate::ReportSaveMode::Off => "off",
        crate::ReportSaveMode::Always => "always",
        crate::ReportSaveMode::FailureOnly => "fail-only",
    }
}

pub fn point_in_rect(x: u16, y: u16, rect: Rect) -> bool {
    x >= rect.x
        && y >= rect.y
        && x < rect.x.saturating_add(rect.width)
        && y < rect.y.saturating_add(rect.height)
}

pub fn truncate_to_width(input: &str, max_width: usize) -> String {
    if UnicodeWidthStr::width(input) <= max_width {
        return input.to_owned();
    }

    let mut out = String::new();
    let mut width = 0usize;
    for ch in input.chars() {
        let ch_width = UnicodeWidthChar::width(ch).unwrap_or(0);
        if width + ch_width >= max_width.saturating_sub(1) {
            break;
        }
        out.push(ch);
        width += ch_width;
    }
    out.push('…');
    out
}
