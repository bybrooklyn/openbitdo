# Homebrew Packaging

Formula source lives in `Formula/openbitdo.rb`.
Template source used for release rendering: `Formula/openbitdo.rb.tmpl`.

Planned tap:
- `bybrooklyn/homebrew-openbitdo`

Current status:
- release workflow computes checksum-pinned formula values from published assets
- tap sync remains gated by `HOMEBREW_PUBLISH_ENABLED=1`

Optional sync helper:
- `sync_tap.sh` (disabled by default unless `HOMEBREW_PUBLISH_ENABLED=1`)
