package variables

import (
	"fmt"
	"math"
	"sort"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"

	"github.com/frankbardon/lattice/errors"
)

// resolveComputed evaluates every computed (expr-bearing) declaration in decls
// and writes the coerced result into env, in dependency order. env already
// carries the inherited scope plus this node's LITERAL declarations; computed
// declarations are layered on top so an expression may reference inherited
// variables, sibling literals, and other computed variables in the same scope.
//
// Resolution order comes from a Kahn-style topological sort over the dependency
// graph (a computed var depends on every same-scope computed name its
// expression references). A cycle among those vars -> fail-fast VAR_CYCLE. Each
// expression is evaluated against the values resolved so far and the result is
// coerced/validated to the declared type (VAR_EXPR on compile/eval failure,
// VAR_TYPE on a type mismatch). path identifies the declaring node.
//
// env is mutated in place (it is the freshly-built child map owned by Extend).
func resolveComputed(decls []Declaration, env Environment, path string) error {
	// Index this scope's computed declarations by name. A later declaration of a
	// name already shadowed an earlier one in env, so the winner is the one to
	// evaluate.
	computed := make(map[string]Declaration)
	for _, d := range decls {
		if d.isComputed() {
			computed[d.Name] = d
		}
	}
	if len(computed) == 0 {
		return nil
	}

	// Compile each expression and derive its dependency set, restricted to
	// computed names in this scope (references to inherited vars or local
	// literals are already resolvable and impose no ordering constraint).
	progs := make(map[string]*vm.Program, len(computed))
	deps := make(map[string]map[string]bool, len(computed))
	indeg := make(map[string]int, len(computed))
	for name, d := range computed {
		prog, err := compileExpr(d, env, path)
		if err != nil {
			return err
		}
		progs[name] = prog

		refs, err := referencedNames(d, path)
		if err != nil {
			return err
		}
		ds := make(map[string]bool)
		for _, ref := range refs {
			// A reference to a same-scope computed name (INCLUDING the variable
			// itself) is a dependency edge. A self-reference yields an in-degree
			// that can never reach zero, so it surfaces as a cycle below.
			if _, ok := computed[ref]; ok {
				ds[ref] = true
			}
		}
		deps[name] = ds
		indeg[name] = len(ds)
	}

	// Kahn's algorithm over a sorted ready-set for deterministic ordering.
	queue := zeroIndegree(indeg)
	resolved := 0
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		resolved++

		d := computed[name]
		val, err := evalAndCoerce(progs[name], d, env, path)
		if err != nil {
			return err
		}
		env[name] = ResolvedVar{
			Name:       d.Name,
			Type:       d.Type,
			Default:    val,
			Expr:       d.Expr,
			Options:    d.Options,
			DeclaredAt: path,
		}

		for other, ds := range deps {
			if ds[name] {
				indeg[other]--
				if indeg[other] == 0 {
					queue = insertSorted(queue, other)
				}
			}
		}
	}

	if resolved < len(computed) {
		// The unresolved remainder forms one or more cycles.
		var cyc []string
		for name := range computed {
			if indeg[name] > 0 {
				cyc = append(cyc, name)
			}
		}
		sort.Strings(cyc)
		return errors.NewCodedErrorWithDetails(errors.VAR_CYCLE,
			"computed variable expressions form a dependency cycle",
			map[string]any{"path": path, "names": cyc})
	}
	return nil
}

// compileExpr compiles d.Expr with undefined identifiers allowed: variable
// references are bound at run time from the resolved env, so the type checker is
// left untyped on purpose. This keeps the supported syntax surface generic
// (operators are not constrained at compile time per E3-S3); type enforcement
// happens on the RESULT via coerce. A syntax error is a fail-fast VAR_EXPR.
func compileExpr(d Declaration, _ Environment, path string) (*vm.Program, error) {
	program, err := expr.Compile(d.Expr, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.VAR_EXPR,
			"computed variable expression failed to compile",
			map[string]any{"path": path, "name": d.Name, "expr": d.Expr})
	}
	return program, nil
}

// referencedNames parses d.Expr and returns the distinct top-level identifiers
// it references. A parse failure is a fail-fast VAR_EXPR error (it would also
// have failed compilation, but parsing for dependencies happens first).
func referencedNames(d Declaration, path string) ([]string, error) {
	tree, err := parser.Parse(d.Expr)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.VAR_EXPR,
			"computed variable expression failed to parse",
			map[string]any{"path": path, "name": d.Name, "expr": d.Expr})
	}
	seen := make(map[string]bool)
	var names []string
	c := &identCollector{fn: func(id string) {
		if !seen[id] {
			seen[id] = true
			names = append(names, id)
		}
	}}
	ast.Walk(&tree.Node, c)
	return names, nil
}

// identCollector visits an expr AST and reports every identifier name.
type identCollector struct {
	fn func(string)
}

func (c *identCollector) Visit(node *ast.Node) {
	if id, ok := (*node).(*ast.IdentifierNode); ok {
		c.fn(id.Value)
	}
}

// evalAndCoerce runs the compiled program against the current env values and
// coerces the result to d.Type, validating it. An evaluation failure is
// VAR_EXPR; a result that cannot be coerced is VAR_TYPE (or VAR_OPTIONS_INVALID
// for an out-of-set enum, via validateValue).
func evalAndCoerce(prog *vm.Program, d Declaration, env Environment, path string) (any, error) {
	out, err := expr.Run(prog, exprValues(env))
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.VAR_EXPR,
			"computed variable expression failed to evaluate",
			map[string]any{"path": path, "name": d.Name, "expr": d.Expr})
	}

	val, err := coerce(out, d.Type)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.VAR_TYPE,
			"computed variable result does not match its declared type",
			map[string]any{"path": path, "name": d.Name, "type": string(d.Type)})
	}

	loc := fmt.Sprintf("%s.variables[%s]", path, d.Name)
	if err := validateValue(val, d, loc); err != nil {
		return nil, err
	}
	return val, nil
}

// exprValues builds the run-time environment: the in-scope variable values (the
// resolved Default slot, which holds literal defaults and already-computed
// results alike).
func exprValues(env Environment) map[string]any {
	out := make(map[string]any, len(env))
	for name, rv := range env {
		out[name] = rv.Default
	}
	return out
}

// coerce normalizes an expr-lang result into the canonical decoded-JSON shape
// the rest of the pipeline expects (numbers as float64, arrays as []any) and
// rejects a result whose Go kind is incompatible with the declared type. The
// supported expr syntax surface stays generic: coercion constrains only the
// RESULT type, not the operators used to produce it.
func coerce(v any, t VarType) (any, error) {
	switch t {
	case VarTypeString, VarTypeEnum:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", v)
		}
		return s, nil
	case VarTypeBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", v)
		}
		return b, nil
	case VarTypeNumber, VarTypeInteger:
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", v)
		}
		if t == VarTypeInteger && f != math.Trunc(f) {
			return nil, fmt.Errorf("expected integer, got fractional %v", f)
		}
		return f, nil
	case VarTypeArray:
		arr, ok := toAnySlice(v)
		if !ok {
			return nil, fmt.Errorf("expected array, got %T", v)
		}
		return arr, nil
	case VarTypeObject:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object, got %T", v)
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported declared type %q", t)
	}
}

// toFloat converts the numeric kinds expr may yield into float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toAnySlice converts an expr array result into []any.
func toAnySlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, true
	default:
		return nil, false
	}
}

// zeroIndegree returns the names with no unresolved computed dependency, sorted
// for deterministic evaluation order.
func zeroIndegree(indeg map[string]int) []string {
	var ready []string
	for name, n := range indeg {
		if n == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	return ready
}

// insertSorted inserts name into the already-sorted queue, keeping it sorted so
// evaluation order stays deterministic.
func insertSorted(queue []string, name string) []string {
	i := sort.SearchStrings(queue, name)
	queue = append(queue, "")
	copy(queue[i+1:], queue[i:])
	queue[i] = name
	return queue
}
