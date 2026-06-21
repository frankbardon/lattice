// Package service is the SOLE public package of lattice: a transport-agnostic
// facade over the dashboard cores (resolver, changeset, storage, variables).
//
// THE BOUNDARY. Everything under internal/ is non-importable outside the module
// by Go's internal-package rule. The cores deliberately stay there so their
// shapes remain free to change; service is the one place a stable public surface
// is exposed. External callers — a CLI, an HTTP/MCP server, a WASM host —
// program against service.* alone and never name an internal/... path. Each
// transport is a thin adapter over this facade; the facade owns the wiring
// (filesystem abstraction, schema catalog, store/resolver construction) so the
// load → resolve → apply → save dance lives in exactly one place.
//
// CONSTRUCTION. There are two ways to build a Service, both returning the same
// wired facade:
//
//   - Open(Options{Backend, Root, Schemas}) — the batteries-included path for
//     real-filesystem callers. It builds the writable store over an OS filesystem
//     rooted at Root and a resolver over the Schemas fs.FS (an os.DirFS in a CLI),
//     then wires them. This is the one most programs want.
//
//   - New(store, res) — the injection path, taking an already-built Store and
//     *Resolver and wiring them without touching the filesystem. Pair it with the
//     low-level builders NewStore(backend, fs, root) and NewResolver(schemas
//     fs.FS) to supply a custom or in-memory store and a resolver over embedded
//     schemas (an embed.FS). This is the path a WASM or MCP frontend uses, where
//     the document and schema catalog live in memory rather than on a disk.
//
// VERBS. A constructed *Service exposes the whole transport-agnostic surface:
//
//   - Read:    Resolve(id, overrides) and ResolveBytes(b, src, overrides) run the
//     two-pass resolver (store-addressed vs in-memory); Load(id),
//     List(), Exists(id) are byte-level store reads.
//   - Write:   ParseChangeset(b) yields an opaque *Changeset; Patch(id, cs, opts)
//     applies it through the atomic apply→validate→re-resolve→save
//     pipeline (pass WithExpectedRevision(rev) for an optimistic-
//     concurrency precondition); Save(document) writes UNVALIDATED whole
//     bytes; Delete(id) removes a document.
//   - History: History(id) and LoadAt(id, rev) read version history (git backend
//     only); Revision(id) returns the current revision token to pair with
//     WithExpectedRevision. These are capability-gated — a backend that
//     lacks the capability is rejected with STORAGE_CAPABILITY_UNSUPPORTED
//     rather than degrading silently.
//
// OPAQUE HANDLES. The boundary types this package re-exports (ResolvedTree,
// Changeset, ApplyResult, Store, …) are type ALIASES of their internal
// definitions, so a caller can name and READ them through service.* but cannot
// CONSTRUCT the ones with unexported fields itself. Values of those types are
// produced only by the constructors and methods above. This keeps construction —
// and therefore invariants like validation, atomicity, and byte-faithfulness —
// under the facade's control.
//
// ERRORS. Every failure path returns a *errors.CodedError from the lattice
// errors package (github.com/frankbardon/lattice/errors), which is itself at the
// module root and so already public — no alias is needed. A CodedError carries a
// stable Code, a human message, and a structured Details map, and marshals to the
// JSON envelope a --json transport emits. Service methods reuse the existing Code
// vocabulary; capability-absence is reported as a "*_UNSUPPORTED" code rather
// than a silent skip.
package service

import (
	"github.com/frankbardon/lattice/internal/changeset"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
	"github.com/frankbardon/lattice/internal/variables"
)

// The aliases below re-export the internal boundary types so external callers
// name only service.* and never an internal/... path. They are ALIASES (=), not
// new named types, so a service.ResolvedTree IS the resolver.ResolvedTree the
// cores produce — no conversion is needed at the seam.
//
// CONSTRAINT: an alias lets a caller name and read a boundary type, but the types
// carrying unexported fields (Resolver, Store implementations, …) still cannot be
// constructed outside their defining package. Callers obtain values of these
// types only from the constructors (Open / New / NewStore / NewResolver) and the
// Service methods this package provides, so the facade retains control over how
// they are built and what invariants they satisfy. They are, in effect, opaque
// handles.

// ResolvedTree is the durable, JSON-serializable output of resolution: the
// document manifest plus the recursively resolved root instance, produced only
// after both validation passes succeed. See resolver.ResolvedTree for the full
// contract and field-level documentation.
type ResolvedTree = resolver.ResolvedTree

// ResolvedInstance is one node of the resolved tree: a single item instance with
// its type reference resolved to a canonical, versioned identity, plus its
// validated config, placement, layout, children, and per-node scopes. It is
// re-exported so a caller can name and READ a tree's nodes (e.g. ResolvedTree.Root
// and a node's Children) through service.* without naming an internal/... path.
// See resolver.ResolvedInstance.
type ResolvedInstance = resolver.ResolvedInstance

// ResolvedTypeRef is the resolved identity of an item type as referenced by an
// instance: the raw $ref, the canonical id it resolved to, and the parsed
// name/version. It is re-exported so callers can read a ResolvedInstance.Type
// without naming an internal/... path. See resolver.ResolvedTypeRef.
type ResolvedTypeRef = resolver.ResolvedTypeRef

// ConfigurableField is one entry of a node's editable CONFIGURABLE SURFACE
// (E3-S1): a config field the item type declares runtime-configurable, with its
// value type, label, optional constraints, and optional preferred widget
// rendering. It is re-exported so a caller can name and READ a node's editable
// surface (the NodeView surface get_node returns) through service.* without naming
// an internal/... path. See resolver.ConfigurableField.
type ConfigurableField = resolver.ConfigurableField

// Resolver validates dashboard documents and emits resolved trees (the two-pass
// resolver). It carries unexported state and is constructed only via the facade.
// See resolver.Resolver.
type Resolver = resolver.Resolver

// Changeset is a parsed, validated RFC 6902 JSON Patch addressed to a single
// document. See changeset.Changeset; obtain one via the facade's parse path.
type Changeset = changeset.Changeset

// ApplyResult is the outcome of a successful changeset apply: the validated,
// canonically-serialized document bytes that were persisted plus the resolved
// tree of that persisted document. See changeset.ApplyResult.
type ApplyResult = changeset.ApplyResult

// ApplyOption configures a changeset apply (the stable seam for additive apply
// inputs, e.g. the revision precondition). See changeset.ApplyOption. Construct
// values with the re-exported WithExpectedRevision.
type ApplyOption = changeset.ApplyOption

// Store is the core persistence contract: a dumb, byte-faithful blob store that
// reads and writes whole dashboard documents addressed by manifest.id. See
// storage.Store.
type Store = storage.Store

// RevisionedStore is the OPTIONAL capability exposing a single current-revision
// token per document (implemented by both backends), used for the
// optimistic-concurrency precondition. Detect it with a type assertion on a
// Store. See storage.RevisionedStore.
type RevisionedStore = storage.RevisionedStore

// VersionedStore is the OPTIONAL capability exposing read-side version history
// (git backend only). Detect it with a type assertion on a Store. See
// storage.VersionedStore.
type VersionedStore = storage.VersionedStore

// Revision identifies one stored version of a document — the element type of the
// slice History returns. It carries the revision Hash (the token LoadAt accepts),
// the commit Message, and the Timestamp. It is re-exported so callers can name
// History's []Revision return without naming an internal/... path. See
// storage.Revision.
type Revision = storage.Revision

// Backend names the persistence backend kind selected when constructing a Store.
// The known values are the BackendFS and BackendGit consts re-exported below. See
// storage.Backend.
type Backend = storage.Backend

const (
	// BackendFS selects the filesystem-backed Store. It is the default. See
	// storage.BackendFS.
	BackendFS = storage.BackendFS

	// BackendGit selects the git-backed Store: filesystem write semantics plus a
	// commit per Save/Delete and read-side version history. See storage.BackendGit.
	BackendGit = storage.BackendGit
)

// OverrideSet is the unified, addressable runtime-override carrier: a map from
// override address (a bare variable name, or a "<node-id>.<field>" node+field
// address) to value, handed to resolution. See variables.OverrideSet.
type OverrideSet = variables.OverrideSet

// WithExpectedRevision records the revision the caller expects the stored
// document to currently be at — the optimistic-concurrency precondition. When
// supplied, the apply re-reads the store's current revision immediately before
// the write and rejects with CHANGESET_REVISION_CONFLICT on mismatch. It is
// re-exported as a FUNCTION (not a type alias) so callers build an ApplyOption
// without naming the internal package. See changeset.WithExpectedRevision.
func WithExpectedRevision(revision string) ApplyOption {
	return changeset.WithExpectedRevision(revision)
}

// Service is the transport-agnostic facade: a single value that holds the wired
// store and resolver and exposes the read, write, and history methods over them
// (see the package doc for the verb set). It is constructed via Open or New; its
// fields are unexported so callers cannot assemble one with an unwired or
// inconsistent store/resolver pair.
type Service struct {
	// store is the wired persistence backend (filesystem or git), the single
	// blob store every read and write goes through.
	store Store

	// resolver is the wired two-pass resolver used to validate documents and emit
	// resolved trees, including the re-resolution the write pipeline performs.
	resolver *Resolver
}
