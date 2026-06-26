#!/usr/bin/env bash
# .claude/flow/gates/pre-pr-ready.sh
# Runs before flow-implement flips the effort PR draft → ready. Non-zero exit blocks.
# Captured by flow-backfill on 2026-06-26.
set -euo pipefail

make build
