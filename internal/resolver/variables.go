package resolver

import (
	"encoding/json"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/variables"
)

// buildVariableModel decodes the variable declarations from the raw document and
// returns the document-scope environment plus the raw instance tree, which the
// instance walk threads down to compute each node's scoped environment (E3-S1)
// and interpolate its config (E3-S2).
//
// Variable declarations are authored at document scope and on instances, but the
// typed schema.Document/Instance structs (owned upstream) do not carry them, so
// this re-decodes the raw document bytes into a generic view walked in lockstep
// with the resolved tree during resolveInstance. Document-scope declarations are
// validated here (fail-fast, the first invalid one surfaces as a VAR_* error);
// per-instance declarations are validated as the walk extends each node's scope.
func buildVariableModel(data []byte) (variables.Environment, *rawInstance, error) {
	var raw struct {
		Variables []variables.Declaration `json:"variables"`
		Root      *rawInstance            `json:"root"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		// The document already passed structural validation, so this is an
		// internal inconsistency rather than an authoring error.
		return nil, nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed re-decoding document for variable resolution")
	}

	// Document-scope declarations form the outermost environment.
	docEnv, err := variables.Environment(nil).Extend(raw.Variables, "doc")
	if err != nil {
		return nil, nil, err
	}
	return docEnv, raw.Root, nil
}

// rawInstance is the minimal generic view of an instance: just its variable
// declarations and children, walked in lockstep with the resolved tree. All
// other fields are ignored.
type rawInstance struct {
	Variables []variables.Declaration `json:"variables"`
	Children  []*rawInstance          `json:"children"`
}
