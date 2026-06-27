---
name: revisions
description: Optimistic concurrency for the propose-then-commit loop — the opaque revision token from lattice_get_outline / lattice_get_node / lattice_validate_patch's baseRevision, passing it as `expectedRevision` on POST /api/patch to avoid clobbering a concurrent write (409 CHANGESET_REVISION_CONFLICT), and the capability-gated reality that the token is best-effort (may be absent; STORAGE_CAPABILITY_UNSUPPORTED on stores without RevisionedStore) — and what to do then.
type: guide
kind: workflow
applies_to: [lattice_get_outline, lattice_get_node, lattice_validate_patch]
---

# Revisions

Lattice's edit loop is **propose (MCP) → commit (human, out of band)**. A
revision token is how that hand-off stays safe across the gap: it lets the
human's commit detect that the stored document **moved** between your read and
their write, and refuse to clobber the concurrent change. This skill covers the
token, the `expectedRevision` precondition, conflict handling, and the
capability-gated caveat. For the surrounding procedure read **authoring-loop**;
MCP itself never writes.

## The opaque revision token

Three read/simulate tools surface the document's **current** revision:

- **`lattice_get_outline {id}`** → `revision` (top-level field).
- **`lattice_get_node {id, nodeId}`** → `revision` (top-level field).
- **`lattice_validate_patch {id, ops}`** → `baseRevision` (in the success result) — the
  same value, named for its role as the *base* the eventual write builds on.

The token is **opaque**: treat it as **compare-only**. Never parse it, derive
meaning from it, infer ordering from it, or fabricate one. Its only use is to be
echoed back to the write endpoint as a precondition.

## The `expectedRevision` precondition

The single write path is **`POST /api/patch`** (HTTP, served by `lattice serve`
— never reached from MCP). Its body:

```
POST /api/patch
{"id": "<id>", "ops": [<id-rooted RFC 6902 patch>], "expectedRevision": "<baseRevision>"}
```

`expectedRevision` is an **optimistic-concurrency precondition**. The commit
checks the stored document's current revision against it:

- **Match** → the document is unchanged since you read it; the patch commits.
  The response is `{revision: "<new>", result: <resolved tree>}` — note the
  **new** revision (the old one is now stale; any further edit must re-read).
- **Mismatch** → the document moved on since `baseRevision`; the commit is
  **rejected** with `409 CHANGESET_REVISION_CONFLICT` rather than overwriting the
  concurrent change.

The field is a **pointer / optional**: omitting it skips the precondition
entirely (last-writer-wins). Passing the `baseRevision` from your latest
`lattice_validate_patch` is exactly what makes the hand-off safe — always thread it
through.

## Handling a `409` conflict

A `409 CHANGESET_REVISION_CONFLICT` is **not** a malformed patch — it means a
concurrent write landed first. Do **not** retry blindly with the same
`expectedRevision` (it will conflict again, and force-committing without the
precondition could clobber the other change). Instead **re-run the loop against
the new state**:

1. Re-read — `lattice_get_outline {id}` (and `lattice_get_node` if drilling) to pick up the
   **current** revision and the post-concurrent-write structure.
2. Re-build / re-simulate — rebuild the cumulative ops against the new tree and
   `lattice_validate_patch` until `ok: true`, capturing the **new** `baseRevision`.
3. Re-commit — the human re-issues `POST /api/patch` with the fresh
   `expectedRevision`.

This rebases your edit onto the concurrent change instead of silently
overwriting it.

> Other write-endpoint statuses are distinct: an unknown id is `404`
> (`STORAGE_NOT_FOUND`); a malformed / off-surface / structurally invalid
> changeset is `422`. Only `409` is a revision conflict.

## Best-effort & capability-gated — the caveat

The revision token is **capability-gated and best-effort**. A current-revision
token is the optional `RevisionedStore` capability:

- **Both shipped backends (`fs` and `git`) implement it**, so a normal `lattice
  mcp --store fs|git …` deployment always yields a token.
- A **custom / injected store** that lacks the capability does not. The facade's
  `Revision(id)` then returns a `STORAGE_CAPABILITY_UNSUPPORTED` coded error.

Crucially, the read tools treat a revision miss as **non-fatal**: `lattice_get_outline`
and `lattice_get_node` simply **omit** the `revision` field rather than failing the whole
call (the outline/node read is still useful without it). So **an absent
`revision` is expected, not an error.**

What to do when the token is absent:

- **Still propose and validate normally.** `lattice_validate_patch` works without a
  revision; its `baseRevision` is likewise simply omitted.
- **The commit proceeds without the precondition.** With no token to pass,
  `POST /api/patch` is sent **without** `expectedRevision` — a last-writer-wins
  commit. The concurrency guard is unavailable on that store, so flag the
  reduced safety to the human rather than fabricating a token.
- **Prefer a revisioned backend** (`--store fs` or `--store git`) when the safe
  hand-off matters; `git` additionally gives per-edit commit history.

## Cross-links

- **authoring-loop** — the full read → simulate → commit procedure this safety
  mechanism sits inside (the commit is step 7).
- **patch-authoring** — shaping the id-rooted ops the commit carries.
- **session-bootstrap** — MCP proposes/simulates; the human commits.
