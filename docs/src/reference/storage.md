# Storage Backends

`lattice` can load and save whole dashboard documents through a **storage
backend**. The store is deliberately simple: it reads and writes complete
documents as raw bytes, addressed by each document's `manifest.id`. Two backends
ship today — a plain **filesystem** backend and a **git** backend that records a
commit per change and exposes read-side version history.

Storage sits **upstream** of the resolver. A backend never validates a document,
never resolves `$ref`s, and has no JSON Patch awareness — it treats every
document as an opaque blob. The bytes you save are the bytes you load back.

## The `Store` model

Every backend satisfies one small contract: whole-document load/save plus a few
metadata operations.

| Operation | Behavior |
| --- | --- |
| `Load(id)` | Returns the stored document bytes for a `manifest.id`. A missing id is a `STORAGE_NOT_FOUND` error. |
| `Save(document)` | Persists a whole document. The addressing key is read from the document's own `manifest.id` — it is **not** a separate argument. |
| `List()` | Returns the `manifest.id`s of all stored documents, sorted. |
| `Exists(id)` | Reports whether a document with that id is stored (a cheap existence check, no read). |
| `Delete(id)` | Removes the stored document. A missing id is a `STORAGE_NOT_FOUND` error. |

Two properties define the model:

- **Whole-document (dumb blob).** A backend operates on the entire document as
  `[]byte`. It performs no partial edits and no schema validation. A document
  saved then loaded is **byte-identical**, which keeps git diffs clean.
- **Addressed by `manifest.id`.** `Save` derives the key from the document
  itself; callers never choose where the bytes land. This is what makes backends
  interchangeable — the same call persists to the filesystem or to git depending
  only on which backend was constructed.

All failures are `CodedError`s with `STORAGE_*` codes (see
[Error Codes](error-codes.md#storage_--whole-document-persistence)), carrying
structured `Details` such as the offending `id` or `path`.

## Filesystem backend (`fs`)

The default backend. It maps a document's `manifest.id` to
`<root>/<id>.json` and is the one selected when you load a document by path
(see [CLI usage](#cli-usage) below).

- **Filename mapping.** A document with `manifest.id` of `example-minimal` is
  stored as `example-minimal.json` under the configured root. `List` recovers
  ids by stripping the `.json` extension from the files directly under root (a
  missing root is treated as empty, not an error).
- **Atomic writes.** `Save` writes to a temporary file in the root, then renames
  it over the destination. A crash mid-write never leaves a partially written
  document — you either have the old bytes or the new bytes, never a torn file.
- **Filename-safe id validation.** Because the id becomes a filename stem, it is
  validated before any write or read. An id that is empty/whitespace-only,
  contains a path separator (`/` or `\`), or is a relative path element (`.` or
  `..`) is rejected with `STORAGE_ID_INVALID`.

The filesystem backend has **no versioning** — it does not implement the
`VersionedStore` capability described below. Each `Save` overwrites the prior
document in place.

## Git backend (`git`)

The git backend is "filesystem write semantics, plus a commit". Documents live
on disk as plain `<id>.json` files in a working-tree git repository — the same
byte-faithful, atomic, id-mapped writes the filesystem backend performs — and
every `Save` or `Delete` records a commit.

- **Reads come from the working tree.** `Load`, `List`, and `Exists` are served
  straight from the on-disk files via the embedded filesystem backend, so a git
  store reads identically to a filesystem store over the same root.
- **Commit on save / commit on delete.** `Save` writes the file, stages it, and
  commits with a generated message (`Save dashboard <id>`). `Delete` removes the
  file, stages the deletion, and commits (`Delete dashboard <id>`).
- **Init on absent root.** If the root is not already a git repository it is
  initialised as a **non-bare, working-tree** repo; if it already is one, it is
  opened. (go-git is used directly — there is no dependency on a system `git`
  binary.)
- **Author identity.** The commit author is resolved from the repository's git
  config (`user.name` / `user.email`). Whichever field is unset falls back to a
  fixed `lattice <lattice@localhost>` identity, so a freshly initialised repo
  with no configured identity still produces valid, attributable commits.
- **Path-scoped staging.** Only the specific `<id>.json` is ever staged — never
  `git add .`. Unrelated untracked or modified files in the repository are left
  alone, so a lattice store can safely share a working tree with other files.

### Edge behavior: no-op commits are rejected

A git commit needs a tree change. Re-saving **byte-identical** content produces
no change to stage, and go-git rejects the empty commit (`ErrEmptyCommit`)
surfaced as a `STORAGE_IO` error. To record a new revision, the document bytes
must actually differ from the currently committed version.

## Versioning (`VersionedStore`)

Versioning is an **optional capability** layered on top of the core `Store`
contract. Only version-capable backends implement it — today, the git backend.
The filesystem backend does not. Callers detect it with a capability check
rather than assuming it is present.

A **revision** is identified by its git **commit hash** and carries the commit
message and timestamp. The version-capable surface adds two operations:

| Operation | Behavior |
| --- | --- |
| `History(id)` | Returns the revisions that touched `<id>.json`, newest-first, by walking the commit log filtered to that document's path. An id that no commit ever touched (never saved, or unknown) returns `STORAGE_NOT_FOUND` — an empty history is reported as not-found, not as an empty list. |
| `LoadAt(id, revision)` | Returns the document bytes as of a given revision. The revision is a commit hash — the full 40-character hash, or a short hash that resolves unambiguously to one commit. A revision that resolves to no commit, or at which the document does not exist, returns `STORAGE_NOT_FOUND`. |

Because history is path-filtered, one document's saves never inflate another
document's history.

## CLI usage

Both `resolve` and `serve` accept the same backend-selection flags:

| Flag | Meaning | Default |
| --- | --- | --- |
| `--store` | Backend kind: `fs` or `git`. | `fs` |
| `--root` | Root directory for the backend. | `.` (the working directory) |

How the positional argument is interpreted depends on whether you set either
flag:

- **Neither `--store` nor `--root` set — direct-path loading (default).** The
  positional argument is a **filesystem path** to a document, resolved directly.
  This is the pre-existing behavior, and every existing `resolve <path>`
  invocation continues to work unchanged. No backend is constructed.
- **`--store` or `--root` explicitly set — backend-addressed loading.** The
  positional argument is a **`manifest.id`** loaded through the constructed
  backend. The backend is built lazily, only once the argument is known to be an
  id, so a plain path-mode invocation never incurs a backend side-effect (such
  as the git backend's `git init`).

Examples:

```sh
# Direct path (default): resolve a document file on disk.
lattice resolve examples/minimal.json

# Backend-addressed: load the document whose manifest.id is "example-minimal"
# from the ./dashboards filesystem store.
lattice resolve --store fs --root ./dashboards example-minimal

# Same, served read-only over HTTP from a git-backed store.
lattice serve --store git --root ./dashboards example-minimal
```

An unrecognized `--store` value fails with `STORAGE_BACKEND_UNKNOWN`. In `serve`
backend mode the store is re-read on every request, so editing the stored
document is reflected on reload exactly as a path-mode edit is; the render stays
read-only.

## Out of scope

The storage layer is intentionally narrow. The following are **not** implemented
and are deferred future work — do not assume them present:

- **The JSON Patch apply→save pipeline.** The store is a dumb blob store: it
  saves and loads whole documents and has **no JSON Patch awareness**. Applying
  a [changeset](../format/changesets.md) to a document and persisting the result
  is a separate, later effort. There is no write path in `serve` today.
- **A database (DB) adapter.** The shipped backends are the filesystem backend
  and the git backend. A database-backed `Store` is future work; the `Store`
  contract is designed to admit one, but none ships.

When either of these is implemented, this page should be updated rather than left
stale.
