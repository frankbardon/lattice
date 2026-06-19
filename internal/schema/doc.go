// Package schema loads dashboard documents and the item-type schema catalog
// over an afero filesystem and resolves every instance $ref into a fully linked,
// in-memory ResolvedGraph for downstream validation (E1-S4).
//
// Resolution covers three $ref forms:
//
//   - absolute URL ("https://lattice.dev/schemas/items/table/1.0.0") — keyed by
//     $id against the local Catalog (the base URL is an identifier namespace,
//     not a fetch target); an optional URLFetcher backs genuinely remote URLs.
//   - relative path ("items/table/1.0.0", "note.schema.json") — normalized onto
//     the catalog base URL, then looked up as a file under configured relative
//     roots.
//   - inline fragment ("#/$defs/...") — resolved against the document's own
//     $defs.
//
// Versioned $id pinning is enforced: a ref naming a catalogued item type with an
// unavailable version fails fast with SCHEMA_VERSION_MISMATCH; an entirely
// unknown ref fails fast with SCHEMA_REF_UNRESOLVED.
//
// This package performs resolution only. Two-pass JSON Schema validation and
// resolved-tree emission are E1-S4's responsibility and consume ResolvedGraph.
package schema
