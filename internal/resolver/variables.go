package resolver

import (
	"encoding/json"
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/variables"
)

// attachVariableEnvironments computes the tree-scoped variable environment for
// every node and attaches it to the resolved tree (E3-S1).
//
// Variable declarations are authored at document scope and on instances, but the
// typed schema.Document/Instance structs (owned upstream) do not carry them, so
// this pass re-decodes the raw document bytes into a generic view and walks it in
// lockstep with the already-built resolved tree. Each node's environment layers
// declarations root->node with inner-shadows-outer semantics (see
// internal/variables). The walk is fail-fast: the first invalid declaration is
// returned as a VAR_* CodedError naming the offending path.
//
// Interpolation (E3-S2) and computed expr values (E3-S3) are intentionally NOT
// handled here; this pass only builds the model and the scoped environments.
func attachVariableEnvironments(data []byte, tree *ResolvedTree) error {
	var raw struct {
		Variables []variables.Declaration `json:"variables"`
		Root      *rawInstance            `json:"root"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		// The document already passed structural + config validation, so this is
		// an internal inconsistency rather than an authoring error.
		return errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed re-decoding document for variable resolution")
	}

	// Document-scope declarations form the outermost environment.
	docEnv, err := variables.Environment(nil).Extend(raw.Variables, "doc")
	if err != nil {
		return err
	}

	if tree.Root == nil {
		return nil
	}
	return attachNodeEnv(docEnv, raw.Root, tree.Root, "root")
}

// rawInstance is the minimal generic view of an instance: just its variable
// declarations and children, walked in lockstep with the resolved tree. All
// other fields are ignored.
type rawInstance struct {
	Variables []variables.Declaration `json:"variables"`
	Children  []*rawInstance          `json:"children"`
}

// attachNodeEnv extends env with the node's own declarations, stores the result
// on the resolved node, and recurses over children. path is the resolved-tree
// path of node, used both for shadowing provenance and error reporting.
func attachNodeEnv(parentEnv variables.Environment, raw *rawInstance, node *ResolvedInstance, path string) error {
	var decls []variables.Declaration
	if raw != nil {
		decls = raw.Variables
	}

	env, err := parentEnv.Extend(decls, path)
	if err != nil {
		return err
	}
	node.VarEnv = env

	for i, child := range node.Children {
		var rawChild *rawInstance
		if raw != nil && i < len(raw.Children) {
			rawChild = raw.Children[i]
		}
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := attachNodeEnv(env, rawChild, child, childPath); err != nil {
			return err
		}
	}
	return nil
}
