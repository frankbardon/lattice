#!/usr/bin/env bash
# .claude/flow/gates/pre-commit.sh
# Runs before every flow-driven git commit. Non-zero exit blocks the commit.
# Captured by flow-backfill on 2026-06-26.
set -euo pipefail

go fmt ./...
go vet ./...
