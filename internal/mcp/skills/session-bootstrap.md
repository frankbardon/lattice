---
name: session-bootstrap
description: Read this FIRST. Defines the lattice MCP skill pack — its frontmatter schema, the source-layering doctrine (which tool is authoritative for what), the CALL-FIRST get_manifest flow, and the skill-file naming convention. Every other skill assumes you have read this.
type: guide
kind: workflow
applies_to: [get_manifest, list_skills, get_skill]
---

# Session bootstrap

This is the keystone skill of the lattice MCP pack. It establishes the contract
every other skill follows. Read it before doing anything else in a lattice
session. It is intentionally light on lattice's domain grammar — that lives in
the live tools, not here (see **Source layering** below).

## CALL FIRST: `get_manifest`

Begin every session by calling **`get_manifest`**. It is the single orienting
call: it returns the server version, the document ids you can work with, the
item-type catalog the resolver knows, and the skill index. Do not guess at any
of that from memory — the manifest is the ground truth for *this* server, *this*
session.

The opening loop is always:

1. **`get_manifest`** — orient: what server, what documents, what item types,
   what skills exist.
2. **`list_skills`** / **`get_skill`** — pull the skill(s) whose `applies_to`
   matches the tool(s) you are about to use.
3. The live read tools (`get_outline`, `get_node`, `get_schema`) — fetch the
   actual structure / grammar / surface for the document at hand.

Never skip step 1. A skill tells you *how* to work; the manifest tells you *what
is there to work on*.

## Source layering (the doctrine)

Lattice exposes several tools, and each one is **authoritative for exactly one
thing**. Skills describe workflow and intent; they do NOT restate what a live
tool will tell you. Critically, **a skill never re-emits `get_schema` grammar** —
schemas drift per server and per item type, so any copy in a skill would rot.
When you need grammar, call the tool.

| Source | Authoritative for | Use it when |
|---|---|---|
| `get_schema` (+ `list_schemas`) | **Grammar** — the JSON Schema for an item type: its fields, types, enums, required keys | You need to know what a node's config may legally contain |
| `get_outline` | **Structure** — the document's node skeleton: ids, types, nesting, and a node's freeform `metadata` where it carries it | You need to navigate or locate a node |
| `get_node` | **Editable surface** — a single node's current config + which fields are settable | You are about to edit one node and need its present state |
| `validate_patch` | **Truth / simulate** — applies a proposed RFC 6902 changeset in a dry run and reports the resolved result or the coded error; never persists | You want to confirm an edit is valid *before* it touches anything |
| `POST /api/patch` (HTTP, not MCP) | **The only write** — the sole path that persists a change | The human commits a validated change |

Rules that fall out of this:

- **Schemas are never copied into skills.** Ask `get_schema`. If a skill seems to
  describe a field, treat it as intent/usage, and confirm the actual grammar with
  the tool.
- **MCP never writes.** Every MCP tool is read-only or simulate-only.
  `validate_patch` *simulates* a change and shows you the outcome, but it persists
  nothing. The single write path is `POST /api/patch`, an HTTP endpoint outside
  the MCP surface, driven by the human. Propose and validate through MCP; the
  human commits through the write endpoint.
- **`validate_patch` is the proof step.** Before suggesting a change be written,
  run it through `validate_patch` and report what it resolved to (or the coded
  error it produced). Do not claim an edit is correct on inspection alone.
- **The catalog is open.** The item types `get_schema` knows are not a fixed
  built-in set — a downstream server can publish *first-class custom types* by
  tagging a schema with the `latticeBehavior` keyword, and they appear in
  `get_manifest` / `list_schemas` like any built-in. Never assume the catalog from
  memory; read it. How to author one is **custom-item-types**.

## Frontmatter schema

Every skill file opens with a `---`-delimited YAML frontmatter block. The fields:

| Key | Required | Values | Meaning |
|---|---|---|---|
| `name` | yes | identifier | Stable name; the `get_skill` argument. Matches the file stem. |
| `description` | yes | one line | Summary shown in `list_skills` / `get_manifest` so a host can pick without fetching the body. |
| `type` | yes | `guide` \| `reference` | Register: `guide` is a how-to/workflow narrative; `reference` is a lookup catalog consulted on demand. |
| `kind` | yes | `workflow` \| `items` \| `tool` | Shape: `workflow` is an ordered procedure; `items` is a per-item catalog; `tool` focuses on one MCP tool. |
| `applies_to` | yes | list | MCP tool names (or flows) this skill is relevant to, e.g. `[get_node, validate_patch]`. |
| `covers` | optional | list | For a catalog reference: the item types / tools it enumerates. Omit when not a catalog. |

List values accept either inline-bracket (`[a, b, c]`) or bare CSV (`a, b, c`)
form. `name` defaults to the file stem if omitted, but always set it explicitly.

## Skill-file naming convention

- One skill per file, `kebab-case.md`, the stem equal to the `name` frontmatter
  key (`session-bootstrap.md` → `name: session-bootstrap`).
- The stem is the `get_skill` argument — callers pass the name without the `.md`
  extension.
- Name by intent, not implementation: a workflow guide reads as the task
  (`edit-a-node`), a per-tool guide as the tool (`validate-patch`), a catalog
  reference as the corpus it lists (`item-types`).
- Keep grammar OUT of the file. If you find yourself transcribing a schema, stop
  and point the reader at `get_schema` instead.
