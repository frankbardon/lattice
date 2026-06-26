#!/usr/bin/env bash
# .claude/flow/gates/pre-push.sh
# Runs before every flow-driven git push. Non-zero exit blocks the push.
# Captured by flow-backfill on 2026-06-26.
set -euo pipefail

go test ./...
make lint
