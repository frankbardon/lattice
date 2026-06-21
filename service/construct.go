package service

import (
	"io/fs"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
)

// dashboardSchemaFile is the dashboard document schema's filename within the
// schemas filesystem; it is loaded for the structural (Pass 1) validation. It
// mirrors the same const in internal/cli so the schema-loading behavior is
// identical across the CLI and the facade (parity through the cutover).
const dashboardSchemaFile = "dashboard.schema.json"

// schemasRoot is the catalog directory scanned for the dashboard schema and
// item-type schemas. The schemas fs.FS supplied to Open/NewResolver is adapted
// to an afero.Fs whose root IS that filesystem's root, so the dashboard schema
// lives directly at dashboardSchemaFile and the catalog is scanned from ".".
//
// This is the one rooting assumption: callers root Options.Schemas where the
// schema files are directly accessible (e.g. os.DirFS("schemas") or an embed.FS
// sub-tree whose top level holds dashboard.schema.json and the item-type
// catalog), NOT a parent directory that contains a "schemas/" subdirectory. The
// CLI achieves the equivalent by passing the "schemas" directory as the catalog
// dir over an OsFs rooted at the working directory.
const schemasRoot = "."

// Options configures the batteries-included Open constructor. It names the
// writable store (Root + Backend) and the read-only schema catalog (Schemas).
//
// Schemas is the stdlib fs.FS holding the dashboard schema and item-type
// catalog; it is adapted to the cores' afero.Fs via afero.FromIOFS for
// read-only schema loading. Real-filesystem callers pass os.DirFS(dir); a WASM
// host can pass an embed.FS. The dashboard schema must be directly accessible at
// dashboardSchemaFile within Schemas (see schemasRoot).
type Options struct {
	// Root is the root directory for the writable store backend, interpreted by
	// the backend over the real filesystem (afero OsFs).
	Root string

	// Backend names the persistence backend kind (BackendFS or BackendGit). The
	// zero value is BackendFS — the filesystem-backed store.
	Backend Backend

	// Schemas is the read-only schema catalog filesystem (dashboard schema +
	// item-type schemas), adapted to afero via afero.FromIOFS.
	Schemas fs.FS
}

// Open is the batteries-included constructor for real-filesystem callers. It
// assembles the writable store over an afero OsFs rooted at opts.Root and a
// resolver over opts.Schemas (adapted read-only via afero.FromIOFS), then wires
// them into a Service. Schema-load/parse failures surface as the existing
// SCHEMA_* coded errors; an unknown backend surfaces as the storage factory's
// coded error.
//
// The WASM in-memory write side does not use Open: it constructs an in-memory
// Store and a resolver from embedded schemas itself and wires them via New.
func Open(opts Options) (*Service, error) {
	store, err := NewStore(opts.Backend, afero.NewOsFs(), opts.Root)
	if err != nil {
		return nil, err
	}

	res, err := NewResolver(opts.Schemas)
	if err != nil {
		return nil, err
	}

	return New(store, res), nil
}

// New is the injection constructor: it wires an already-constructed store and
// resolver into a Service without touching the filesystem. It is the path for
// custom or in-memory stores (tests, the WASM write side) and pairs with the
// low-level builders NewStore and NewResolver. The arguments are produced only
// by the facade's constructors, so the wired pair is always consistent.
func New(store Store, res *Resolver) *Service {
	return &Service{
		store:    store,
		resolver: res,
	}
}

// NewResolver builds a two-pass resolver over the read-only schema catalog in
// schemas. The stdlib fs.FS is adapted to the cores' afero.Fs via
// afero.FromIOFS (read-only); the dashboard schema is loaded from
// dashboardSchemaFile and the catalog is scanned from the filesystem root (see
// schemasRoot). It replicates the internal/cli wiring so the CLI cutover is
// parity-preserving.
//
// Schema-load/parse failures are returned as *errors.CodedError with the
// existing SCHEMA_IO / SCHEMA_INVALID codes.
func NewResolver(schemas fs.FS) (*Resolver, error) {
	afs := afero.FromIOFS{FS: schemas}

	dashSch, err := loadDashboardSchema(afs)
	if err != nil {
		return nil, err
	}

	return resolver.New(afs, dashSch, []string{schemasRoot})
}

// NewStore is a thin wrapper over the storage factory: it constructs the Store
// for the given backend over fs rooted at root. It exists so external callers
// build a Store without naming an internal/... path (the injection path for
// New). An unknown backend surfaces as the factory's coded error.
func NewStore(backend Backend, fs afero.Fs, root string) (Store, error) {
	return storage.New(backend, fs, root)
}

// loadDashboardSchema reads and parses the dashboard document schema from
// dashboardSchemaFile on fs. It mirrors internal/cli.loadDashboardSchema's
// behavior and error codes (SCHEMA_IO on read failure, SCHEMA_INVALID on parse
// failure) so the facade and the CLI load the schema identically.
func loadDashboardSchema(fs afero.Fs) (*jsonschema.Schema, error) {
	data, err := afero.ReadFile(fs, dashboardSchemaFile)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading dashboard schema "+dashboardSchemaFile)
	}
	var s jsonschema.Schema
	if err := s.UnmarshalJSON(data); err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_INVALID, "failed parsing dashboard schema "+dashboardSchemaFile)
	}
	return &s, nil
}
