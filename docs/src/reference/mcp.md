# MCP Mode

`lattice` can run as an **MCP ([Model Context Protocol](https://modelcontextprotocol.io/))
server**, exposing its read and dry-run capabilities as tools an MCP host (a
coding assistant, an agent, Claude Desktop, …) can call. The host uses these
tools to **discover, read, and propose edits** to stored dashboard documents — but
the MCP server **never persists**. A proposed edit is committed separately, by a
human, through the [`POST /api/patch`](#committing-the-change-post-apipatch) HTTP
endpoint. This split is the heart of MCP mode: the model *proposes and validates*;
the human *commits*.

> **Security: no authentication anywhere.** Neither the MCP stdio server nor the
> `POST /api/patch` write endpoint performs any authentication or authorization.
> MCP mode assumes a **localhost / trusted** deployment. Do not expose either on
> an untrusted network. This is a known, accepted gap — see
> [No auth — known gap](#no-auth--known-gap).

## Running `lattice mcp`

The `mcp` subcommand runs the server over **stdio** — the transport an MCP host
uses to launch a local server as a subprocess and talk to it over stdin/stdout.

```sh
lattice mcp --store fs --root ./dashboards --schemas schemas
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--store` | `fs` | Backend kind: `fs` or `git`. |
| `--root` | `.` | Root directory the backend reads documents from. |
| `--schemas <dir>` | `schemas` | Directory holding `dashboard.schema.json` and the item/connection catalog. |
| `--json` | off | Emit an invocation/transport error as a `{code,message,details}` JSON envelope. |

Unlike `serve`, `mcp` **always** operates through a backend: its tools address
documents by `manifest.id`, never by filesystem path, so the `--store`/`--root`
seam (shared with [`resolve`](../getting-started/building-and-running.md#resolve),
[`serve`](../getting-started/building-and-running.md#serve), and `patch`) is always
used. The `fs` backend maps `<id>` to `<root>/<id>.json`; the `git` backend adds a
current-revision token per document (needed for the optimistic-concurrency
handoff below).

The process prints `lattice MCP server running on stdio (Ctrl-C to stop)` and
then blocks, serving the host until it disconnects or the process is interrupted.

### Host configuration

An MCP host launches the server by running the `lattice` binary with the `mcp`
subcommand. A typical host config (the shape Claude Desktop and most MCP hosts
accept — a named server with a `command` and `args`) points at the built binary:

```json
{
  "mcpServers": {
    "lattice": {
      "command": "/absolute/path/to/bin/lattice",
      "args": [
        "mcp",
        "--store", "fs",
        "--root", "/absolute/path/to/dashboards",
        "--schemas", "/absolute/path/to/schemas"
      ]
    }
  }
}
```

Use **absolute paths** — the host launches the subprocess with its own working
directory, which is rarely your project root. To get commit history per edit
(and the strongest revision handoff), use `"--store", "git"` instead.

## The tool reference

The server advertises seven tools, all **read or dry-run only** — none writes to
the store. Every tool surfaces a failure as the lattice
[`CodedError`](error-codes.md) (code + message + structured `Details`) verbatim,
returned as an MCP tool error so the model can read the code and self-correct.

| Tool | Purpose | When to use |
| --- | --- | --- |
| [`list_dashboards`](#list_dashboards) | Enumerate stored documents (id + title). | First step — discover what exists. |
| [`get_outline`](#get_outline) | Config-free skeleton of one document + revision. | Navigate a document cheaply; locate a node by id. |
| [`get_node`](#get_node) | One node's stored subtree + editable field surface. | Read a node you intend to **edit in place**. |
| [`get_document`](#get_document) | Whole document, raw and (optionally) resolved. | Escape hatch — only when a slice won't do. |
| [`list_schemas`](#list_schemas) | The grammar catalog (item types + envelope). | Discover what node types you may **build**. |
| [`get_schema`](#get_schema) | One type's JSON Schema. | Author a **new** node/document validly. |
| [`validate_patch`](#validate_patch) | Dry-run a changeset; never persists. | Check a proposed edit before a human commits it. |

### `list_dashboards`

The discover-and-enumerate entry point.

- **Input:** none.
- **Output:** `{dashboards: [{id, title?}]}` — every stored document's
  `manifest.id` (the key every other tool accepts) plus its `manifest.title` when
  present. The title is best-effort: a document whose bytes can't be read still
  appears, listed by id with no title.
- **When:** the first call in any session — to learn which ids exist.

### `get_outline`

The token-cheap navigation tool. Resolves a document server-side and returns a
**config-free skeleton** of the tree — no config bodies, so the host can locate a
target node without pulling the whole (config-laden) document through context.

- **Input:** `{id}` — the document's manifest id.
- **Output:**
  - `id` — the requested id.
  - `revision` — the document's current opaque revision token (omitted when the
    store has no revision capability).
  - `document` — a document-scope summary: `{variables: [names], connections:
    [ids], theme: bool}`. Names and ids only — no value bodies.
  - `root` — the skeleton root node, recursively. Each node carries `id`, `type`
    (short item-type ref), optional `title` (only when the node's config declares
    one), `container` (true when it may hold children), an optional `placement`
    **summary** (e.g. `"col 2+1, row 1+1"`, never the verbatim placement object),
    and `children`.
- **When:** to navigate. Use the outline to find the `id` of the node you want,
  then drill in with `get_node` — *not* `get_document`, whose config bodies the
  outline deliberately omits.

### `get_node`

The drill-in read for **editing an existing node**. Given a document id and a
node id (from the outline), it returns the exact stored shape a patch edits plus
the set of field paths that are valid to patch.

- **Input:** `{id, nodeId}` — the document id and the node's stable instance id.
- **Output:**
  - `id`, `nodeId` — echoed.
  - `revision` — the document's current revision token (omitted when unsupported).
  - `subtree` — the **stored JSON subtree** for the node: the exact shape a raw
    id-rooted patch edits.
  - `surface` — the node's **editable field surface** as a flat `[{key, type}]`
    list. `key` is the tail of a valid id-rooted patch path (nested keys dotted,
    e.g. `grid.gap`); `type` is the field's value type.
- **Block addressing:** when `nodeId` names a block wrapper, the `subtree` is the
  whole block (wrapper plus its config/content) and the `surface` is the **content
  item's** editable fields — a block delegates its knobs to what it wraps.
- **Surface gates field edits only.** The `surface` lists which *field* paths a
  patch may touch. Structural edits (add/remove/move children) are **not**
  surface-listed — plan those from `get_outline`.
- **When:** to edit a node that already exists. The `surface` tells you which
  field paths `validate_patch` will accept. (Distinct errors make the two failure
  modes clear: an unknown document id is `STORAGE_NOT_FOUND`; an unknown node id
  is `CHANGESET_TARGET_NOT_FOUND`.)

### `get_document`

The whole-document **escape hatch**. Prefer the slicing tools (`get_outline` +
`get_node`) for targeted reads; reach for this only when you genuinely need the
entire document.

- **Input:** `{id, resolved?}` — the document id, and an optional flag.
- **Output:** `{id, document, resolved?}` — `document` is the raw stored JSON;
  `resolved` is the full resolved tree, present **only** when `resolved: true` was
  requested (it runs the two-pass resolver).
- **When:** rarely — when no slice suffices. Pulling a whole document is the
  expensive path the outline/node tools exist to avoid.

### `list_schemas`

The grammar-discovery tool: what node types **may** be built, independent of any
existing node.

- **Input:** none.
- **Output:** `{types: [string]}` — every item-type name in the schema catalog,
  plus the reserved `"dashboard"` envelope token. Every entry is a valid
  `get_schema` input.
- **When:** before **building** a new node or document — to discover the legal
  type tokens.

### `get_schema`

The grammar-detail tool: one type's JSON Schema, so a **new** node of that type
(or a whole new dashboard) can be authored validly.

- **Input:** `{type}` — an item-type name from `list_schemas`, or `"dashboard"`
  for the envelope.
- **Output:** `{type, schema}` — the type's JSON Schema (config fields, required
  keys, `$ref` form) as JSON.
- **When:** when **building new**. The contrast with `get_node` is the key
  distinction:
  - **Editing an existing node →** `get_node` (its `surface` tells you which
    field paths are valid to patch).
  - **Building a new node →** `get_schema` (its schema tells you the full config
    grammar a new node must satisfy).

### `validate_patch`

The **simulate** step of the propose-then-commit loop. It runs the same atomic
apply → re-resolve pipeline a real write runs, under every guardrail — but
**stops before the store write**. It **never persists**: there is no save path
reachable through MCP at all.

- **Input:** `{id, ops}` — the document id and the cumulative RFC 6902 JSON Patch
  array. Pointers are **id-rooted**: each op's `path` leads with a node's stable
  id or a `$`-scope keyword (`$manifest`, `$variables`, `$connections`, `$theme`,
  `$root`), and the remainder is literal [RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)
  (see [Changesets](../format/changesets.md)). The server is **stateless** — send
  the **full cumulative patch** on every call, not an incremental delta.
- **Output (success):** `{ok: true, preview, baseRevision?}` — `preview` is the
  resolved tree the patch *would* produce (nothing is persisted to produce it);
  `baseRevision` is the document's current revision token, the value the eventual
  write passes as `expectedRevision`.
- **Output (failure):** `{ok: false}` plus the pipeline's coded error (e.g.
  `PATCH_*` for a malformed op set, a configurable-surface or structural rejection,
  or a re-resolution `RESOLVE_*`/`SCHEMA_*`/`VAR_*`) as a tool error. Correct the
  ops and call again.
- **When:** to check a proposed edit. Iterate `validate_patch` until `ok: true`,
  then hand `baseRevision` to the human for the commit. **The model stops here** —
  it cannot and does not persist.

## The propose-then-commit flow

The whole point of MCP mode is a clean split between the model (which proposes and
validates an edit) and the human (who commits it). The end-to-end loop:

1. **List.** `list_dashboards` → pick the target document `id`.
2. **Navigate.** `get_outline {id}` → locate the node `id` to change; note the
   `revision` and the document-scope summary.
3. **Drill or discover.**
   - Editing an existing node → `get_node {id, nodeId}` for the stored `subtree`
     and the editable `surface` (which field paths are patchable).
   - Building a new node → `list_schemas` then `get_schema {type}` for the config
     grammar a new node must satisfy.
4. **Validate (iterate).** `validate_patch {id, ops}` with the cumulative,
   id-rooted RFC 6902 patch. On a coded error, correct the ops and re-validate;
   repeat until `ok: true`. Keep the returned `baseRevision`. **Nothing has been
   persisted.**
5. **Commit (human).** A human commits the validated patch via
   [`POST /api/patch`](#committing-the-change-post-apipatch), passing the
   `baseRevision` from step 4 as `expectedRevision`. This is the only persistence
   path.

### Committing the change (`POST /api/patch`)

The commit happens outside MCP, against the `serve` HTTP layer running in backend
mode (`lattice serve --store … --root … <id>`). The endpoint:

```
POST /api/patch
{"id": "<id>", "ops": [<RFC 6902 id-rooted JSON Patch>], "expectedRevision": "<baseRevision from validate_patch>"}
```

It commits the changeset through the same atomic apply → validate → save pipeline
`validate_patch` simulated, and returns `{"revision": "<new>", "result":
<resolved tree>}` on success. The `expectedRevision` is an
**optimistic-concurrency precondition**: if the stored document moved on since
`validate_patch` read `baseRevision`, the commit is rejected with `409`
(`CHANGESET_REVISION_CONFLICT`) — re-run the flow against the new revision. An
unknown id is `404`; a malformed/off-surface/invalid changeset is `422`. Each
error is the coded-error JSON envelope. (The field is optional; omitting it skips
the precondition, but passing the `baseRevision` is what makes the handoff safe.)

> **The MCP server never reaches this endpoint.** `POST /api/patch` is served by
> the `serve` command, not the `mcp` command. The model's last step is a
> successful `validate_patch`; a human performs the commit.

## No auth — known gap

There is **no authentication or authorization** on either MCP mode surface:

- the `lattice mcp` stdio server (any host that can launch the subprocess can call
  every tool), and
- the `POST /api/patch` write endpoint on `lattice serve` (any caller that can
  reach the port can mutate stored documents).

MCP mode assumes a **localhost / trusted** deployment. The read/dry-run-only MCP
tools cannot persist, so the exposure there is read-only; the write endpoint,
however, mutates state with no caller check. **Do not expose either on an
untrusted network.** This is a deliberate, documented boundary for this effort,
not an oversight — revisit the trust assumption before adding a remote transport.

## A compile-checked walkthrough

The MCP tools are thin wrappers over the public [`service`](../getting-started/library.md)
facade. The exact facade calls each tool makes — `List`, `Resolve`, `NodeView`,
`ListSchemas`/`Schema`, `ParseChangeset` + `DryRunPatch` (the dry-run behind
`validate_patch`), and `Revision` — are demonstrated as a runnable, compile-checked
Go example in
[`service/mcp_example_test.go`](https://github.com/frankbardon/lattice/blob/main/service/mcp_example_test.go).
Because it is a real `Example` function, it cannot drift from the facade
signatures the tools depend on.
