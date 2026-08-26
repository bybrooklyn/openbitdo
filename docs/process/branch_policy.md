# Branch Policy

## Defaults

- default branch: `main`
- automation/worktree branches: `codex/*`
- release tags: `v*`

## Merge Expectations

- clean-room implementation stays in `cleanroom/`
- dirty-room or decompiler material never lands in runtime, tests, docs, or workflows inside this tree
- release tags are created from commits that are already on `main`

## Review Checklist

- clean-room guard passes
- no forbidden path references were introduced
- docs and release metadata are consistent with the current RC
- required CI checks stay green
