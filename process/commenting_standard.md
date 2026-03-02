# OpenBitdo Commenting Standard

This project prefers concise, high-context comments.

## Required Comment Zones
- Command gating order and rationale (`bitdo_proto::session`).
- Support-tier decisions and promotion boundaries (`candidate-readonly` vs `full`).
- Unsafe/firmware blocking rules and brick-risk protections.
- Retry/fallback behavior where multiple command paths exist.
- State-machine transitions in TUI/app-core flows when transitions are non-obvious.

## Avoid
- Trivial comments that restate code syntax.
- Comment blocks that drift from behavior and are not maintained.

## Rule of Thumb
If someone adding a new device could misread a policy or safety boundary, comment it.
