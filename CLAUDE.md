# CLAUDE.md

Guidance for working in this repo. Read before changing the MCP layer or the skill pack.

## Project

`lattice` builds and serves dashboard documents. Core flow: a stored JSON document is
**resolved** into a tree, **navigated/edited** via an MCP server (read + simulate only),
and **persisted** through one HTTP write endpoint. Public API is the `service` facade;
everything under `internal/` is private.

- **Build/test:** `make build`, `make test` (`go test ./...`), `make vet`, `make fmt`, `make lint`. Go 1.26, `CGO_ENABLED=0`.
- **Never commit `.planning/`** (workflow scratch; gitignored). Stage explicit paths or `git add -A -- ':!.planning' ':!.planning/**'`.

## Import boundary (non-negotiable)

The MCP surface is split into an SDK-free core and a single SDK-coupled adapter:

- `mcp/*` (the core) imports ONLY the `service` facade, `github.com/google/jsonschema-go` (schema
  reflection — a module separate from the protocol SDK), the module-root `errors` package, and
  `mcp/skills`. It **NEVER** imports the MCP SDK (`github.com/modelcontextprotocol/go-sdk`). This is
  enforced by `mcp/firewall_test.go`, which `go list -deps` the core and fails if the SDK appears in
  its transitive set — a build-blocking regression, not a style nit.
- `mcp/gosdk` is the **only** package allowed to import the go-sdk. It is the transport adapter:
  `Register(server, svc, version)` mounts the core's descriptor catalog + `lattice-skill://*`
  resources onto a caller-supplied `*sdkmcp.Server`. `internal/cli/mcp.go` dogfoods it.
- `mcp/skills` is **stdlib-only** pure embedded data (so the core may import it without breaching the
  boundary). Keep it that way.
- The schema reflector **panics on recursive Go types**. Type nested/recursive output struct fields as
  `any` (see `mcp/tool_read.go`, `mcp/tool_outline.go`).

## The MCP skill pack — what it is

`mcp/skills/*.md` is an **LLM-facing** skill pack served over MCP (`list_skills`,
`get_skill`, the `get_manifest` index, and `lattice-skill://` resources). Skills are embedded
via `//go:embed *.md`, so **adding a `.md` file is all it takes to publish a skill** — no
registration. `mcp/skills/session-bootstrap.md` is the keystone and the canonical
statement of these conventions; this file enforces them, it does not replace them.

### Source-layering doctrine (the core rule)

Each live tool is authoritative for exactly one thing. **Skills describe workflow and intent;
they NEVER restate what a live tool returns.** In particular, **a skill must never copy
`get_schema` grammar** (fields/types/enums) — it drifts per server and per item type and would
rot. When grammar is needed, the skill points the reader at the tool.

| Source | Authoritative for |
|---|---|
| `get_schema` / `list_schemas` | Grammar (item-type JSON Schema: fields, types, enums) |
| `get_outline` | Structure (node skeleton: ids, types, nesting, placement) |
| `get_node` | Editable surface (one node's config + settable fields) |
| `validate_patch` | Truth (dry-run an RFC 6902 changeset; never persists) |
| `POST /api/patch` (HTTP, not MCP) | The only write path (human commits) |

### Frontmatter schema (every skill file)

```yaml
name:         # required — kebab id, equals the file stem, is the get_skill argument
description:  # required — one line; shows in list_skills / get_manifest
type:         # required — guide | reference
kind:         # required — workflow | items | tool
applies_to:   # required — list of MCP tools/flows this skill is relevant to
covers:       # optional — for catalog refs: the item types/tools it enumerates
```

### Naming

One skill per `kebab-case.md`; stem == `name`. Name by intent, not implementation. Keep grammar
out of the body.

## ENFORCEMENT: changing the system requires updating skills

**Any change to the surfaces below MUST update the matching skill(s) and the manifest catalog in
the same change.** A PR that alters behavior without its skill update is incomplete. When in
doubt, re-read `session-bootstrap.md` and the affected skill, then reconcile.

| You change… | You MUST also update… |
|---|---|
| Add / rename / remove an MCP tool (`mcp/tool_*.go`) | (1) add a typed handler + a `NewTool(...)` registration in the `Tools(cfg)` catalog (`mcp/mcp.go`) — `get_manifest` DERIVES its catalog from those descriptors, so there is **nothing to hand-sync** for the manifest; (2) every skill whose `applies_to` names that tool; (3) if it changes the author loop, the `authoring-loop` skill and the source-layering table in `session-bootstrap.md`. If the tool is reachable over the transport, add its SDK registration via `mcp/gosdk` (never import the SDK from the core). |
| Bump the build version wiring | nothing in skills; version flows as a parameter — `internal/cli/mcp.go` passes it to `gosdk.Register`, which threads it into `mcp.Config{Version}` so `get_manifest` reports it. There is no `serverVersion` global to keep in sync. |
| Add / remove / change an item type (`schemas/items/*.schema.json`) | the matching family skill's `covers` + guidance: `items-layout` (container, block), `items-content` (markdown, heading, image), `items-inputs` (the input widgets), `items-forms` (form, configurator). Do NOT copy field grammar — only intent/"pick this when". |
| Change changeset / patch semantics (`internal/changeset/`) | `patch-authoring` (id-rooted pointers, field vs structural edits, surface gating) — re-verify its inline example still validates |
| Change variable resolution (`internal/variables/`) | `variables` (`${name}` template vs `{"$var":...}` binding, scope) |
| Change theme, connections, placement, blocks, or revision behavior | the matching workflow skill: `theming`, `connections`, `placement-grid`, `blocks`, `revisions` |
| Change the write endpoint (`internal/server`, `POST /api/patch`) | `authoring-loop`, `revisions`, and the `POST /api/patch` row in `session-bootstrap.md` |
| Add a brand-new skill | valid frontmatter (above); terse LLM-authored prose; obey source-layering (no `get_schema` grammar); cross-link related skills/tools. It auto-publishes via `go:embed`. |
| Add a new coded error for the MCP layer | add it in `errors/codes.go` (MCP domain block, e.g. `MCP_SKILL_NOT_FOUND`); return it verbatim from handlers (never flatten to a string) |

After any skills/MCP change, run `go test ./mcp/...` (skills-loader + tool tests + the
`firewall_test.go` import check) plus `make build` and `make vet`. Skill tests assert "≥
session-bootstrap present / sorted / non-empty", so adding skills must not break them; if you add an
exact-count assertion, keep it in sync.
