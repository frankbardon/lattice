package variables

import (
	"sort"

	"github.com/frankbardon/lattice/errors"
)

// Environment is the set of variables VISIBLE at a single node, keyed by name.
// It is the shadowing winner per name: the value for a name is the nearest
// declaration on the path from the document root to the node. A nil/empty
// Environment means no variables are in scope at that node.
//
// Environment is the per-node artifact attached to the resolved tree. Because
// each ResolvedVar records DeclaredAt, the full var->node visibility mapping is
// recoverable from the tree alone (no separate index needed), which is what
// makes future dependency-tracked partial re-resolution possible.
type Environment map[string]ResolvedVar

// Names returns the visible variable names in sorted order, for deterministic
// iteration and diagnostics.
func (e Environment) Names() []string {
	if len(e) == 0 {
		return nil
	}
	names := make([]string, 0, len(e))
	for n := range e {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the visible variable for name and whether it is in scope.
func (e Environment) Lookup(name string) (ResolvedVar, bool) {
	v, ok := e[name]
	return v, ok
}

// Extend layers a node's own declarations onto the parent environment, returning
// a NEW environment (the parent is never mutated, so sibling subtrees stay
// independent). A declaration on this node SHADOWS an inherited one of the same
// name. path is the declaring node's resolved-tree path, recorded as DeclaredAt.
//
// Declarations are validated before they enter the environment; the first
// invalid declaration is returned as a CodedError (fail-fast). A duplicate name
// WITHIN a single node's declarations is rejected as a declaration error.
//
// Computed declarations (those carrying an expr, E3-S3) are layered LAST, after
// every literal in this scope is in place, and resolved in dependency order so
// an expression may reference inherited variables, sibling literals, and other
// computed variables (resolveComputed; a cycle fails fast with VAR_CYCLE).
func (e Environment) Extend(decls []Declaration, path string) (Environment, error) {
	if len(decls) == 0 {
		// No local declarations: the node sees exactly its parent's scope. Return
		// the parent map directly; callers must treat environments as read-only.
		return e, nil
	}

	next := make(Environment, len(e)+len(decls))
	for k, v := range e {
		next[k] = v
	}

	seen := make(map[string]bool, len(decls))
	for i, d := range decls {
		if err := validateDeclaration(d, path, i); err != nil {
			return nil, err
		}
		if seen[d.Name] {
			return nil, errors.NewCodedErrorWithDetails(errors.VAR_DECLARATION_INVALID,
				"variable name is declared more than once on the same node",
				map[string]any{"path": path, "name": d.Name})
		}
		seen[d.Name] = true

		// Computed values are filled in by resolveComputed below; placing the
		// declaration with a nil value first keeps the name in scope (so its
		// shadowing of an inherited var is correct) without a premature value.
		next[d.Name] = ResolvedVar{
			Name:       d.Name,
			Type:       d.Type,
			Default:    d.Default,
			Expr:       d.Expr,
			Options:    d.Options,
			DeclaredAt: path,
		}
	}

	// E3-S3: evaluate computed declarations in dependency order, overwriting
	// their placeholder entries with the coerced expression results.
	if err := resolveComputed(decls, next, path); err != nil {
		return nil, err
	}
	return next, nil
}
