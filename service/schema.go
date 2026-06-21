package service

import (
	"path"
	"sort"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// itemsDir is the catalog subdirectory holding the item-type schemas within the
// schemas filesystem. An item type's catalog name is the stem of its
// <name>.schema.json file under this directory (e.g. items/block.schema.json ->
// "block"); that stem is the type token get_schema accepts and list_schemas
// enumerates.
const itemsDir = "items"

// schemaFileSuffix is the trailing portion of an item-type schema filename. A
// file under itemsDir is an item-type schema iff its name ends with this suffix;
// the leading portion is the type name.
const schemaFileSuffix = ".schema.json"

// dashboardSchemaType is the reserved type token that addresses the dashboard
// ENVELOPE schema (dashboardSchemaFile) rather than an item type. ListSchemas
// includes it in the catalog and Schema returns the dashboard schema for it.
const dashboardSchemaType = "dashboard"

// ListSchemas returns the dashboard grammar catalog: every item-type name
// available under the schemas filesystem's items directory plus the reserved
// dashboardSchemaType envelope token. The names are the tokens Schema accepts.
// The item names are sorted; the dashboard token is appended last so the
// envelope is always discoverable.
//
// It reads from the SAME filesystem the resolver was constructed over (the
// single source of truth) — it does not re-open the schemas directory or
// duplicate any file. A read failure surfaces as a *errors.CodedError with
// SCHEMA_IO.
func (s *Service) ListSchemas() ([]string, error) {
	fs := s.resolver.SchemaFS()

	entries, err := afero.ReadDir(fs, itemsDir)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading item schema catalog "+itemsDir)
	}

	names := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= len(schemaFileSuffix) || name[len(name)-len(schemaFileSuffix):] != schemaFileSuffix {
			continue
		}
		names = append(names, name[:len(name)-len(schemaFileSuffix)])
	}
	sort.Strings(names)

	return append(names, dashboardSchemaType), nil
}

// Schema returns the raw JSON Schema bytes for the given grammar type. A type of
// dashboardSchemaType ("dashboard") returns the dashboard envelope schema; any
// other token names an item type and returns items/<type>.schema.json. The bytes
// are returned verbatim from the schemas filesystem — the SAME source the
// resolver validates against — so callers see the authoritative schema, not a
// copy.
//
// An unknown type (no matching item-type schema file) surfaces as a
// *errors.CodedError with SCHEMA_NOT_FOUND, the requested type reported in
// Details["type"]; a read failure surfaces as SCHEMA_IO.
func (s *Service) Schema(typ string) ([]byte, error) {
	fs := s.resolver.SchemaFS()

	var file string
	if typ == dashboardSchemaType {
		file = dashboardSchemaFile
	} else {
		file = path.Join(itemsDir, typ+schemaFileSuffix)
	}

	data, err := afero.ReadFile(fs, file)
	if err != nil {
		if exists, _ := afero.Exists(fs, file); !exists {
			return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_NOT_FOUND,
				"no schema for type "+typ, map[string]any{"type": typ})
		}
		return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading schema "+file)
	}

	return data, nil
}
