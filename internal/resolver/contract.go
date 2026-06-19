package resolver

// This file implements the E4-S3 RESULT-SHAPE CONTRACT pass: it closes the
// data-binding loop at the spec level. An item-type schema may declare an
// `expectedResult` keyword — a JSON Schema fragment describing the rows/fields
// the item expects from its connection. For every item that binds to a
// connection (E4-S2), the resolver:
//
//   - requires the item type to declare (or inherit) an expectedResult, and
//     that the declared fragment is WELL-FORMED (compiles as draft 2020-12);
//     a missing/malformed contract fails fast.
//   - for `static` connections only, validates the connection's inline data
//     against that expectedResult. A static connection embeds its rows in
//     config, so it is the one place a real shape check is possible without a
//     live fetch. Live connections are model-only: the declared contract is
//     validated, never fetched data.
//   - records the resolved contract (item type, connection id, expected result)
//     on the binding so the resolved tree carries it.
//
// expectedResult is a SCHEMA-LEVEL keyword, not per-instance config: it lives on
// the item-type schema and is surfaced by google/jsonschema-go as an unknown
// keyword in Schema.Extra. It is ignored by ordinary config validation. This
// pass runs inside the binding walk (see binding.go), after the connection pass,
// because it needs both the resolved connections and each item's resolved type.
//
// The pass lives in its own file (per file-ownership rules); binding.go invokes
// it per bound node.

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// expectedResultKey is the reserved item-type schema keyword that declares an
// item's result-shape contract. It is a top-level keyword on the item-type
// schema (not an instance-config property), captured by google/jsonschema-go as
// an unknown keyword in Schema.Extra.
const expectedResultKey = "expectedResult"

// staticConnectionRowsKey is the config key under which a static connection
// embeds its inline result rows (see schemas/connections/static.schema.json).
const staticConnectionRowsKey = "rows"

// staticTypeName is the connection-type name whose inline config data can be
// shape-checked against an item's expectedResult without a live fetch.
const staticTypeName = "static"

// resolveContract builds (and validates) the result-shape contract for one bound
// item. It is called from the binding walk for every item that declared a
// connectionId; conn is the resolved connection the item binds to (already known
// to exist). It returns the contract to attach to the binding, or the first
// CodedError (fail-fast):
//
//   - CONTRACT_MISSING  — the bound item's type declares no expectedResult.
//   - CONTRACT_INVALID  — the declared expectedResult is not a well-formed schema.
//   - RESULT_SHAPE_INVALID — a static connection's inline data violates the shape.
func resolveContract(g *schema.ResolvedGraph, inst *ResolvedInstance, conn *ResolvedConnection, path string) (*ResolvedContract, error) {
	expected, ok := expectedResultFor(g, inst.Type.ID)
	if !ok {
		return nil, errors.NewCodedErrorWithDetails(errors.CONTRACT_MISSING,
			"bound item type declares no expectedResult result-shape contract",
			map[string]any{"path": path, "type": inst.Type.Name})
	}

	// Compile the fragment to confirm it is well-formed. The compiled schema is
	// reused below to validate static inline data, so the fragment is parsed once.
	compiled, err := compileResultSchema(expected)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.CONTRACT_INVALID,
			"item-type expectedResult is not a well-formed JSON Schema fragment",
			map[string]any{"path": path, "type": inst.Type.Name})
	}

	// Static connections carry their result inline, so this is the one place a
	// real shape check is possible without a fetch. Live connections are
	// model-only: the declared contract above is validated, not fetched data.
	if conn.Type.Name == staticTypeName {
		if rows, present := conn.Config[staticConnectionRowsKey]; present {
			if err := compiled.Validate(rows); err != nil {
				return nil, errors.WrapCodedErrorWithDetails(err, errors.RESULT_SHAPE_INVALID,
					"static connection inline data does not conform to the item's expectedResult",
					map[string]any{"path": path, "connectionId": conn.ID})
			}
		}
	}

	return &ResolvedContract{
		ItemType:       inst.Type.Name,
		ConnectionID:   conn.ID,
		ExpectedResult: expected,
	}, nil
}

// expectedResultFor returns the expectedResult fragment declared by the resolved
// item type identified by typeID, if any. The fragment is the verbatim decoded
// JSON object captured in the item-type schema's Extra keywords. The second
// return is false when the type declares no expectedResult.
func expectedResultFor(g *schema.ResolvedGraph, typeID string) (map[string]any, bool) {
	rt := g.Types[typeID]
	if rt == nil || rt.Schema == nil || rt.Schema.Extra == nil {
		return nil, false
	}
	raw, ok := rt.Schema.Extra[expectedResultKey]
	if !ok {
		return nil, false
	}
	frag, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	return frag, true
}

// compileResultSchema marshals an expectedResult fragment back to JSON, parses
// it as a JSON Schema, and compiles (resolves) it — the well-formedness check.
// The compiled, resolved schema is returned so the caller can validate inline
// static data against it without re-parsing. A parse/compile failure is returned
// bare for the caller to wrap as CONTRACT_INVALID.
func compileResultSchema(frag map[string]any) (*jsonschema.Resolved, error) {
	raw, err := json.Marshal(frag)
	if err != nil {
		return nil, err
	}
	var s jsonschema.Schema
	if err := s.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return s.Resolve(nil)
}
