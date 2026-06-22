package resolver

// This file implements ELEMENT METADATA threading (element-metadata E1-S2):
// metadata is a first-class, passthrough field on the resolved tree. The
// resolver carries a node's declared `metadata` onto its ResolvedInstance and
// the top-level document `metadata` onto the tree root, enforcing the two rules
// the dashboard schema cannot express by itself:
//
//   1. ELIGIBILITY — only the document ROOT, grid/regions-or-wrappers CONTAINERS,
//      and block WRAPPERS may carry metadata. Forms, variable-boxes, configurators,
//      content leaves, and widgets may not. Eligibility is keyed on the
//      `latticeBehavior` role/childPolicy accessors (internal/schema/behavior.go),
//      NEVER on a hardcoded type name (custom-item-types doctrine): a future custom
//      `regions-or-wrappers` region or custom wrapper gets metadata for free.
//   2. SCALAR VALUES — every metadata value must be a scalar (string, number,
//      boolean, or null) even on an eligible node; an object/array value is
//      rejected. The dashboard schema already constrains this structurally, but
//      the resolver re-enforces it with a node-path-bearing coded error.
//
// The resolver never branches on metadata CONTENT — beyond these two guards it is
// pure passthrough.

import (
	"encoding/json"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// metadataEligible reports whether a node of the given resolved type may carry
// element metadata. The rule, keyed purely on the behavior keyword:
//
//	eligible = role == wrapper                                   // block wrapper
//	        || (role == region && childPolicy == regions-or-wrappers)  // container
//
// The document root is handled separately by the caller (it is always eligible).
// rt may be nil (a guarded internal inconsistency), reported as ineligible.
func metadataEligible(rt *schema.ResolvedType) bool {
	if rt == nil {
		return false
	}
	if rt.Role() == schema.RoleWrapper {
		return true
	}
	return rt.Role() == schema.RoleRegion && rt.ChildPolicy() == schema.ChildPolicyRegionsOrWrappers
}

// resolveMetadata validates and returns a node's metadata for attachment to its
// ResolvedInstance. It enforces eligibility (unless isRoot) and the scalar-value
// rule, naming the offending node path. A nil/empty metadata map is a no-op:
// it returns nil so a metadata-free node emits no `metadata` key and resolves
// byte-identically to before.
//
// isRoot marks the document root, which is always eligible regardless of its
// resolved type's role (it conventionally is a container, but need not be).
func resolveMetadata(rt *schema.ResolvedType, metadata map[string]any, path string, isRoot bool) (map[string]any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}

	if !isRoot && !metadataEligible(rt) {
		typeName := ""
		if rt != nil {
			typeName = rt.Name
		}
		return nil, errors.NewCodedErrorWithDetails(errors.METADATA_NOT_ELIGIBLE,
			"metadata is only permitted on the document root, containers, and block wrappers",
			map[string]any{"path": path, "type": typeName})
	}

	if err := checkScalarMetadata(metadata, path); err != nil {
		return nil, err
	}
	return metadata, nil
}

// checkScalarMetadata rejects any non-scalar metadata value (an object or array),
// naming the offending key and node path. Scalars are string, number (float64
// from JSON), boolean, and null (nil); anything else fails fast.
func checkScalarMetadata(metadata map[string]any, path string) error {
	for key, val := range metadata {
		if !isScalarMetadataValue(val) {
			return errors.NewCodedErrorWithDetails(errors.METADATA_VALUE_NOT_SCALAR,
				"metadata values must be scalars (string, number, boolean, or null)",
				map[string]any{"path": path, "key": key})
		}
	}
	return nil
}

// documentMetadata extracts the TOP-LEVEL document `metadata` map (sibling of
// manifest) from the raw document bytes, mirroring documentDefaultTheme. It only
// lifts the verbatim object; eligibility is moot (the document scope is always
// eligible) and the scalar check is applied by the caller. Returns nil when the
// document declares no top-level metadata.
func documentMetadata(data []byte) (map[string]any, error) {
	var raw struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed re-decoding document for metadata resolution")
	}
	return raw.Metadata, nil
}

// isScalarMetadataValue reports whether v is a JSON scalar permitted as a
// metadata value: a string, number, boolean, or null. Decoded JSON objects
// (map[string]any) and arrays ([]any) are the rejected non-scalars.
func isScalarMetadataValue(v any) bool {
	switch v.(type) {
	case nil, string, bool,
		float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		json.Number:
		return true
	default:
		return false
	}
}
