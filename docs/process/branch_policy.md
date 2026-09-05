# Branch Policy

## Defaults

- default branch: `main`
- automation/worktree branches: `codex/*`
- stale worktree branches: `worktree-agent-*` are not release sources and must stay untouched for
  `v0.0.3`
- current release-prep branch: `release/v0.0.3`
- current release tag: `v0.0.3`
- stable release tags: `v*`

## Merge Expectations

- clean-room implementation stays in `cleanroom/`
- dirty-room or decompiler material never lands in runtime, tests, docs, or workflows inside this tree
- release tags are created from commits that are already on `main`
- `v0.0.3` must point to the tested merge commit on `main`, not to a worktree branch or local
  release-prep commit
- `v0.0.3` publishes as a normal (non-prerelease) GitHub release; its release gates are in
  `docs/RC_CHECKLIST.md`

## Review Checklist

- clean-room guard passes
- no forbidden path references were introduced
- docs and release metadata are consistent with the current release tag
- required CI checks stay green
