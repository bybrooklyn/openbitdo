use ratatui::style::{Color, Modifier, Style};

pub fn app_title_style() -> Style {
    Style::default()
        .fg(Color::Cyan)
        .add_modifier(Modifier::BOLD)
}

pub fn screen_title_style() -> Style {
    Style::default()
        .fg(Color::White)
        .add_modifier(Modifier::BOLD)
}

pub fn title_style() -> Style {
    Style::default()
        .fg(Color::Cyan)
        .add_modifier(Modifier::BOLD)
}

pub fn subtle_style() -> Style {
    Style::default().fg(Color::Gray)
}

pub fn muted_style() -> Style {
    Style::default().fg(Color::DarkGray)
}

pub fn positive_style() -> Style {
    Style::default()
        .fg(Color::Green)
        .add_modifier(Modifier::BOLD)
}

pub fn warning_style() -> Style {
    Style::default()
        .fg(Color::Yellow)
        .add_modifier(Modifier::BOLD)
}

pub fn danger_style() -> Style {
    Style::default().fg(Color::Red).add_modifier(Modifier::BOLD)
}

pub fn selected_row_style() -> Style {
    Style::default()
        .fg(Color::White)
        .add_modifier(Modifier::BOLD)
}

pub fn action_label_style(active: bool, enabled: bool) -> Style {
    match (active, enabled) {
        (true, true) => Style::default()
            .fg(Color::Cyan)
            .add_modifier(Modifier::BOLD),
        (false, true) => Style::default().fg(Color::White),
        (_, false) => muted_style(),
    }
}

pub fn action_caption_style(enabled: bool) -> Style {
    if enabled {
        subtle_style()
    } else {
        muted_style()
    }
}

pub fn border_style(active: bool, enabled: bool) -> Style {
    match (active, enabled) {
        (true, true) => Style::default().fg(Color::Cyan),
        (false, true) => Style::default().fg(Color::Gray),
        (_, false) => Style::default().fg(Color::DarkGray),
    }
}

pub fn level_color(level: crate::app::state::EventLevel) -> Color {
    match level {
        crate::app::state::EventLevel::Info => Color::White,
        crate::app::state::EventLevel::Warning => Color::Yellow,
        crate::app::state::EventLevel::Error => Color::Red,
    }
}
