// Package schemas embeds and publishes lattice's core JSON-Schema catalog —
// the dashboard document schema, the item-type catalog, the connection types,
// and the theme vocabulary — so a downstream module that consumes lattice as a
// library inherits the catalog WITHOUT copying the .schema.json files into its
// own tree.
//
// The package is intentionally stdlib-only (just embed): it carries pure data so
// the service facade may re-export it (service.CoreSchemas) and a consumer may
// overlay its own item types on top (service.OverlaySchemas) without ever naming
// an internal/... path. The embedded files keep their on-disk layout, so the FS
// is rooted exactly where service.NewResolver/Open expect: dashboard.schema.json
// at the root and item types at items/<name>.schema.json.
package schemas

import (
	"embed"
	"io/fs"
)

// coreFS holds the embedded catalog. The directory arguments embed recursively;
// README.md is deliberately omitted (it is documentation, not a loadable
// schema, and the catalog walk only consumes *.schema.json).
//
//go:embed dashboard.schema.json items connections theme
var coreFS embed.FS

// FS returns the embedded core schema catalog as a read-only fs.FS, rooted so
// dashboard.schema.json is at "." and item types are at items/<name>.schema.json
// — the same rooting service.NewResolver and service.Open assume. Pass it
// directly as service.Options.Schemas, or overlay custom item types over it via
// service.OverlaySchemas.
func FS() fs.FS { return coreFS }
