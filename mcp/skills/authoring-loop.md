---
name: authoring-loop
description: The end-to-end edit loop against a lattice dashboard — orient (lattice_get_manifest) → locate (lattice_get_outline → lattice_get_node) → read grammar (lattice_get_schema) → build an id-rooted RFC 6902 patch → simulate (lattice_validate_patch) → hand off for commit (POST /api/patch). Names which tool answers which question and when to loop back. Read patch-authoring for the patch grammar itself.
type: guide
kind: workflow
applies_to: [lattice_get_manifest, lattice_list_dashboards, lattice_get_outline, lattice_get_node, lattice_list_schemas, lattice_get_schema, lattice_validate_patch]
---

# Authoring loop

The full read→propose→simulate→commit procedure for editing a stored dashboard.
It assumes you have read **session-bootstrap** (the source-layering doctrine and
the CALL-FIRST `lattice_get_manifest` rule). This skill sequences the live tools; it does
NOT restate their grammar — call the tool. For how to *shape* the patch itself,
read the sibling skill **patch-authoring**.

Core invariant from session-bootstrap: **MCP never writes.** Every tool here is
read-only or simulate-only. You drive the loop up to a green `lattice_validate_patch`,
then a human commits via `POST /api/patch`. You stop at the simulate step.

## The loop

| Step | Tool | The question it answers |
|---|---|---|
| 1. Orient | `lattice_get_manifest` | What server/version, which documents, what item types, which skills? |
| 2. Pick | `lattice_list_dashboards` | Which document id is my target? (id + title) |
| 3. Locate | `lattice_get_outline {id}` | Where is the node? Its id, type, nesting, the doc-scope summary, and the `revision`. |
| 4a. Drill (edit existing) | `lattice_get_node {id, nodeId}` | The node's stored `subtree` + its editable field `surface` — which field paths a patch may touch. |
| 4b. Discover (build new) | `lattice_list_schemas` → `lattice_get_schema {type}` | The full config grammar a NEW node/document of that type must satisfy. |
| 5. Build | — | Author the cumulative id-rooted RFC 6902 ops (see **patch-authoring**). |
| 6. Simulate | `lattice_validate_patch {id, ops}` | Is the patch valid? Returns `{ok, preview, baseRevision}` or a coded error. Iterate. |
| 7. Commit (human) | `POST /api/patch` | The only write. Out of band; you do not call it. |

### 1–2. Orient and pick

`lattice_get_manifest` first, always. It is the ground truth for *this* server — the
document ids, the item-type catalog, the skills index. Then `lattice_list_dashboards`
narrows to the `id` you will work on (every other tool keys off that id).

### 3. Locate — `lattice_get_outline`

`lattice_get_outline {id}` returns a config-free skeleton: each node's `id`, `type`,
`container` flag, placement summary, and children, plus a document-scope summary
(`variables`, `connections`, `theme`) and the current `revision`. Use it to find
the **node id** you will edit, and to plan **structural** edits (where a child
goes, what to move/remove) — structure is what the outline shows. Note the
`revision` for the eventual commit. Prefer the outline over `lattice_get_document`: the
outline omits config bodies on purpose, so it is the token-cheap way to navigate.

### 4. Drill or discover (this fork decides which tool is authoritative)

- **Editing a node that already exists → `lattice_get_node {id, nodeId}`.** It returns
  the stored `subtree` (the exact shape your patch edits) and the editable
  `surface`: a flat `[{key, type}]` list of the field paths that are valid to
  patch. The surface **gates field edits** — `lattice_validate_patch` rejects a field op
  whose path the surface does not list. For a block wrapper, the surface is the
  *content* item's fields (a block delegates its knobs to what it wraps).
- **Building a new node/document → `lattice_get_schema {type}`** (after `lattice_list_schemas`
  to discover the legal type tokens). The schema is the full config grammar a new
  node must satisfy. Do NOT read field grammar from this skill — `lattice_get_schema` is
  authoritative and drifts per server/type.

The surface (`lattice_get_node`) gates **field** edits only. **Structural** edits
(add/remove/move children) are NOT surface-listed — plan those from `lattice_get_outline`
(step 3). See **patch-authoring** for the field-vs-structural split.

### 5–6. Build and simulate — `lattice_validate_patch`

Build the cumulative id-rooted RFC 6902 patch, then call `lattice_validate_patch {id,
ops}`. The server is **stateless**: resend the FULL cumulative patch every call,
not a delta. It runs the same atomic apply→re-resolve pipeline a real write runs,
under every guardrail, but persists nothing.

- Success → `{ok: true, preview, baseRevision}`. `preview` is the resolved tree
  the patch *would* produce; report what it resolved to rather than claiming
  correctness on inspection. Keep `baseRevision` for the commit.
- Failure → `{ok: false}` plus a coded error (`CHANGESET_*`/`PATCH_*` for a
  malformed or off-surface op set; `RESOLVE_*`/`SCHEMA_*`/`VAR_*` from
  re-resolution). **Loop back**: read the code, correct the ops, re-validate.

`lattice_validate_patch` is the proof step. Iterate it until `ok: true` before you hand
anything off.

#### When to loop back

- Off-surface field op (`CONFIG_OVERRIDE_FIELD_UNKNOWN`) → re-read `lattice_get_node`
  surface (step 4a); the path is not patchable as a field.
- Ill-typed value (`CONFIG_OVERRIDE_VALUE_INVALID`) → re-check the surface field's
  `type` (step 4a) or `lattice_get_schema` (step 4b).
- Unknown node id (`CHANGESET_TARGET_NOT_FOUND`) → re-`lattice_get_outline` (step 3); the
  id is wrong or was removed earlier in the same patch.
- Structural reject (grammar/schema on re-resolve) → re-plan placement against
  `lattice_get_outline` (step 3) and the type's `lattice_get_schema` (step 4b).
- Missing/duplicate id on a structural `add` (`CHANGESET_STRUCTURAL_ID_INVALID`)
  → give the added node a non-empty, document-unique `id`.

### 7. Commit (human, out of band)

A human commits the green patch via `POST /api/patch`, the only write path —
served by `lattice serve`, never reached from MCP. The request is `{id, ops,
expectedRevision?}`; pass the `baseRevision` from step 6 as `expectedRevision` so
a document that moved since you read it is rejected (`409`,
`CHANGESET_REVISION_CONFLICT`) instead of being clobbered — then re-run the loop
against the new revision. Your last action is a successful `lattice_validate_patch`.

## Cross-links

- **session-bootstrap** — the doctrine + the CALL-FIRST `lattice_get_manifest` rule.
- **patch-authoring** — how to shape the id-rooted RFC 6902 ops this loop builds.
