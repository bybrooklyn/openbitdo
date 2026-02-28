# Branch and Merge Policy

Because this workspace currently has no active Git repository metadata, this policy is documented for use when repository control is re-enabled.

## Branches
- `codex/dirtyroom-spec`: sanitize findings into `cleanroom/spec` and `cleanroom/process`
- `codex/cleanroom-sdk`: implement SDK and CLI from sanitized artifacts only

## Merge Strategy
- Cherry-pick sanitized spec commits from dirtyroom branch into cleanroom branch.
- Never merge dirty-room evidence paths into cleanroom implementation branch.

## Review Checklist
- Guard script passes.
- No forbidden path references in code/tests.
- Requirement IDs are traceable from implementation and tests.
