use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum QuickAction {
    Refresh,
    Diagnose,
    RunAgain,
    SaveReport,
    RecommendedUpdate,
    EditMappings,
    Settings,
    Quit,
    Confirm,
    Cancel,
    ApplyDraft,
    UndoDraft,
    ResetDraft,
    RestoreBackup,
    Firmware,
    Back,
}

impl QuickAction {
    pub fn label(self) -> &'static str {
        match self {
            QuickAction::Refresh => "Refresh",
            QuickAction::Diagnose => "Diagnose",
            QuickAction::RunAgain => "Run Again",
            QuickAction::SaveReport => "Save Report",
            QuickAction::RecommendedUpdate => "Recommended Update",
            QuickAction::EditMappings => "Edit Mapping",
            QuickAction::Settings => "Settings",
            QuickAction::Quit => "Quit",
            QuickAction::Confirm => "Confirm",
            QuickAction::Cancel => "Cancel",
            QuickAction::ApplyDraft => "Apply",
            QuickAction::UndoDraft => "Undo",
            QuickAction::ResetDraft => "Reset",
            QuickAction::RestoreBackup => "Restore Backup",
            QuickAction::Firmware => "Firmware",
            QuickAction::Back => "Back",
        }
    }
}
