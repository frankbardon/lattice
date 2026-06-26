# flow-* gate scripts

Each `<event>.sh` here runs before the matching flow action. Non-zero exit blocks.

Events: pre-commit, pre-push, pre-pr-open, pre-pr-ready, pre-merge, pre-release.

Active gates (captured 2026-06-26):
- `pre-commit` → `go fmt ./...`, `go vet ./...`
- `pre-push` → `go test ./...`, `make lint`
- `pre-pr-ready` → `make build`

`pre-pr-open`, `pre-merge`, `pre-release` are unset (gate skipped silently).

Re-run `/flow-backfill` (Mode B) to add or edit gates.
