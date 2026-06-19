package variables

import (
	"strings"

	"github.com/frankbardon/lattice/errors"
)

// OverrideKind distinguishes the two kinds of addressable override target an
// OverrideSet may carry.
type OverrideKind int

const (
	// OverrideKindVariable targets a settable variable by name (the existing
	// E3-S4 runtime override). Its value feeds interpolation as override > default
	// exactly as before.
	OverrideKindVariable OverrideKind = iota

	// OverrideKindNodeField targets a node's config field by address
	// (<node-id>.<field-path>). Application/validation of node+field overrides is
	// E4-S2; here the target is parsed and carried only.
	OverrideKindNodeField
)

// nodeFieldSep separates a node id from its field path in a node+field override
// address: "<node-id>.<field-path>" (e.g. "chart-1.title" or
// "panel.grid.gap"). The first separator splits node id from field path; any
// further separators are part of the (dotted) field path.
const nodeFieldSep = "."

// OverrideTarget is a single ADDRESSABLE override target: either a variable name
// (named/scoped) or a node+field address. It is the unified addressing primitive
// E4 builds on — a URL param, a select selection, or a configurator edit all
// resolve to one of these.
//
// Address syntax:
//
//	variable:   "<name>"               — a bare token with no "." (e.g. "region")
//	node+field: "<node-id>.<field>"    — node id, a ".", then a (possibly dotted)
//	                                      field path (e.g. "chart-1.title",
//	                                      "panel.grid.gap")
//
// A target round-trips through String(): ParseOverrideTarget(t.String()) == t for
// every well-formed target.
type OverrideTarget struct {
	// Kind is which sort of thing this target addresses.
	Kind OverrideKind

	// Name is the variable name (OverrideKindVariable) or the node id
	// (OverrideKindNodeField).
	Name string

	// Field is the (possibly dotted) config field path within the addressed node.
	// Empty for OverrideKindVariable.
	Field string
}

// ParseOverrideTarget parses an override address into an OverrideTarget. An
// address with no "." is a variable target; an address of the form
// "<node-id>.<field>" is a node+field target. Parsing is total over the address
// SYNTAX only — whether the named variable or node actually exists is decided
// later by application (variable overrides during the scope walk, node+field
// overrides in E4-S2). A malformed address (empty, or a node+field address
// missing its node id or field path) fails fast with VAR_OVERRIDE_INVALID.
func ParseOverrideTarget(addr string) (OverrideTarget, error) {
	if addr == "" {
		return OverrideTarget{}, errors.NewCodedErrorWithDetails(errors.VAR_OVERRIDE_INVALID,
			"override address is empty", map[string]any{"address": addr})
	}

	node, field, hasField := strings.Cut(addr, nodeFieldSep)
	if !hasField {
		// No separator: a bare variable name.
		return OverrideTarget{Kind: OverrideKindVariable, Name: addr}, nil
	}

	// A separator is present, so this is a node+field address. Both halves must
	// be non-empty: a leading "." (no node id) or a trailing "." (no field path)
	// is malformed.
	if node == "" {
		return OverrideTarget{}, errors.NewCodedErrorWithDetails(errors.VAR_OVERRIDE_INVALID,
			"node+field override address is missing its node id",
			map[string]any{"address": addr})
	}
	if field == "" {
		return OverrideTarget{}, errors.NewCodedErrorWithDetails(errors.VAR_OVERRIDE_INVALID,
			"node+field override address is missing its field path",
			map[string]any{"address": addr})
	}
	return OverrideTarget{Kind: OverrideKindNodeField, Name: node, Field: field}, nil
}

// String renders the target back to its address form, the inverse of
// ParseOverrideTarget for every well-formed target.
func (t OverrideTarget) String() string {
	if t.Kind == OverrideKindNodeField {
		return t.Name + nodeFieldSep + t.Field
	}
	return t.Name
}

// OverrideSet is the UNIFIED, addressable runtime-override carrier (E4-S1): a map
// from override ADDRESS to value. Addresses are interpreted by
// ParseOverrideTarget — a bare name is a variable override, a "<node-id>.<field>"
// address is a node+field override.
//
// It is deliberately a map[string]any so the existing call sites (URL query
// params, the /api/resolve JSON body, the resolve CLI) keep handing the resolver
// a plain string-keyed value map; the resolver classifies each key by address
// rather than the caller pre-sorting them. The variable subset feeds
// interpolation exactly as the old Overrides did; the node+field subset is
// carried for E4-S2.
type OverrideSet map[string]any

// VariableOverrides returns the variable-targeted subset of the set as the
// Overrides map the scope walk consumes (override > default for settable
// variables). Node+field-addressed entries are excluded. A malformed address
// fails fast with VAR_OVERRIDE_INVALID.
//
// This is the seam that keeps E4-S1 a pure carrier refactor: every value that
// flowed through the old Overrides map still flows here, unchanged, because a
// bare variable name parses to an OverrideKindVariable target.
func (s OverrideSet) VariableOverrides() (Overrides, error) {
	if len(s) == 0 {
		return nil, nil
	}
	out := make(Overrides, len(s))
	for addr, val := range s {
		target, err := ParseOverrideTarget(addr)
		if err != nil {
			return nil, err
		}
		if target.Kind == OverrideKindVariable {
			out[target.Name] = val
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// NodeFieldOverrides returns the node+field-targeted subset of the set, keyed by
// parsed target. Application and validation of these overrides is E4-S2; today
// they are merely parsed and carried so a supplied node+field target round-trips
// without breaking variable overrides. A malformed address fails fast with
// VAR_OVERRIDE_INVALID.
func (s OverrideSet) NodeFieldOverrides() ([]NodeFieldOverride, error) {
	if len(s) == 0 {
		return nil, nil
	}
	var out []NodeFieldOverride
	for addr, val := range s {
		target, err := ParseOverrideTarget(addr)
		if err != nil {
			return nil, err
		}
		if target.Kind == OverrideKindNodeField {
			out = append(out, NodeFieldOverride{Target: target, Value: val})
		}
	}
	return out, nil
}

// NodeFieldOverride pairs a parsed node+field target with its supplied value.
// E4-S2 consumes these to mutate the addressed node's config; E4-S1 only carries
// them.
type NodeFieldOverride struct {
	// Target is the parsed node+field address (Kind == OverrideKindNodeField).
	Target OverrideTarget
	// Value is the supplied override value, as decoded (typed) or string (from a
	// URL param). Coercion/validation against the addressed field is E4-S2.
	Value any
}
