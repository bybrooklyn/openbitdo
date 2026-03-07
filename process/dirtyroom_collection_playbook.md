# Dirty-Room Collection Playbook

This playbook describes how dirty-room evidence is gathered and sanitized before the clean-room implementation consumes it.

## Goal

Produce sanitized, requirement-linked evidence that can expand detect, diagnostics, mapping, or firmware understanding without copying vendor material into the runtime.

## Allowed Inputs

- approved decompiler outputs and existing dirty-room transcripts
- official public web pages used for naming or marketing confirmation

## Required Sanitization

Record only structure-level findings:

- command intent
- request and response shape
- validator expectations
- retry or failure behavior
- promotion or safety notes

Do not copy vendor code or raw proprietary snippets.

## Dossier Workflow

1. choose a PID and operation group
2. collect anchors and summarize them
3. create or update the sanitized dossier
4. link the dossier from the relevant matrix rows
5. update evidence indexes and notes

## Promotion Rule

Moving from `read-only candidate` to `supported` requires:

1. static evidence
2. runtime traces
3. hardware confirmation
