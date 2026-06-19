package resolver

// This file implements the DOCUMENT DEFAULT THEME pass (E2-S2): how the resolver
// surfaces a document's document-scope default theme on the resolved tree.
//
// A document may declare a single `theme` at document scope, drawn from the shared
// theme vocabulary (E2-S1). The dashboard schema models that field as a $ref to
// the theme schema, whose tokens are a closed, enum-constrained vocabulary with
// `additionalProperties: false`; the structural pass (Pass 1) therefore REJECTS an
// out-of-vocabulary token — or an out-of-vocabulary value for a known token —
// fail-fast as RESOLVE_DOCUMENT_INVALID, naming the document source. No separate
// theme-token code is introduced: the vocabulary is enforced by JSON-Schema
// validation at config-validate time, reusing the existing document-validation
// path rather than re-checking the same enums a second way.
//
// This pass does NOT merge anything. The default theme is the DEFAULT LAYER only;
// a block wrapper's per-block theme override (E2-S3) is emitted side-by-side on
// its own node, and composing the cascade is left to a downstream consumer. The
// resolver stays dumb here: it reads the verbatim theme object and attaches it.

import (
	"encoding/json"

	"github.com/frankbardon/lattice/errors"
)

// documentDefaultTheme decodes the document-scope default theme from the raw
// document bytes and returns it verbatim for attachment to the resolved tree. The
// schema package's typed Document does not carry the theme field, so (like the
// variable model and connections) it is re-decoded from the raw bytes. The bytes
// have already passed structural validation, so the theme's tokens are known-valid
// against the vocabulary; a decode failure here is an internal inconsistency, not
// an authoring error. Returns nil when the document declares no default theme.
func documentDefaultTheme(data []byte) (map[string]any, error) {
	var raw struct {
		Theme map[string]any `json:"theme"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed re-decoding document for default-theme resolution")
	}
	return raw.Theme, nil
}
