#!/usr/bin/env bash
# .claude/flow/gates/pre-push.sh
# Runs before every flow-driven git push. Non-zero exit blocks the push.
# Captured by flow-backfill on 2026-06-26; scope-aware since 2026-09-30.
#
# Scope (set by flow's gate runner):
#   FLOW_GATE_SCOPE=full (default, and once per effort before the PR goes ready)
#     -> go test ./... then make lint (vet + staticcheck), as before.
#   FLOW_GATE_SCOPE=changed (each test-group checkpoint)
#     -> the same lint + test targets, limited to the packages the effort touched
#        (changed packages + their importers, tests that read changed files by
#        relative path, repo-walking tests). Computed by flow-shared's
#        go-affected-pkgs.sh with .claude/flow/scope.conf; it falls back to
#        ./... whenever it can't be sure. Without flow-shared installed this
#        gate always runs the full suite.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

helper="$HOME/.claude/skills/flow-shared/scripts/go-affected-pkgs.sh"
if [ "${FLOW_GATE_SCOPE:-full}" = changed ] && [ -x "$helper" ]; then
  pkgs="$("$helper" | tr '\n' ' ')" || pkgs="./..."
  if [ -z "${pkgs// /}" ]; then
    echo "pre-push (changed): nothing Go-relevant changed since ${FLOW_BASE_REF:0:8} — skipping lint + tests"
    exit 0
  fi
  echo "pre-push (changed): $pkgs"
  make lint test PKGS="$pkgs"
else
  go test ./...
  make lint
fi
