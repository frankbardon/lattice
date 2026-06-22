package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/service"
)

// The validate_patch tool registered here is the SIMULATE step of the
// propose-then-commit contract (E3-S2): given a document id and the model's
// cumulative id-rooted RFC 6902 changeset, it runs the dry-run primitive
// (service.DryRunPatch) — the SAME atomic apply→re-resolve pipeline a real write
// runs, under EVERY guardrail — but STOPS before the store write. It NEVER
// persists: there is no save path reachable from MCP. A valid patch returns the
// resolved tree it WOULD have produced plus the document's current base revision
// (so the eventual write can pass expectedRevision); an invalid patch surfaces the
// pipeline's *errors.CodedError verbatim as an MCP tool error so the model can
// self-correct and re-validate.
//
// Like the other tools it calls ONLY the ./service facade and surfaces coded
// errors verbatim (returning the error from the handler packs it into the result
// with IsError set; see the SDK's ToolHandlerFor contract), never as a flattened
// plain string.

func init() {
	registrars = append(registrars, registerValidatePatch)
}

// validatePatchInput is the input for validate_patch: the target document's
// manifest id and the cumulative id-rooted RFC 6902 JSON Patch to simulate.
type validatePatchInput struct {
	// ID is the manifest id of the document the patch targets (as listed by
	// list_dashboards).
	ID string `json:"id" jsonschema:"the manifest id of the document the patch targets"`

	// Ops is the RFC 6902 JSON Patch array to simulate, with id-rooted pointers:
	// each op's path leads with a node's stable id or a $-scope keyword
	// ($manifest/$variables/$connections/$theme/$root), remainder literal RFC 6901.
	// Each op is a JSON object ({op, path, value?, from?}) — the changeset parser
	// owns op-shape validation, so this tool models each element as an open object
	// rather than constraining its keys. The server is stateless: send the FULL
	// cumulative patch each call.
	Ops []map[string]any `json:"ops" jsonschema:"the cumulative RFC 6902 JSON Patch (id-rooted pointers) to simulate, each op a {op, path, value?, from?} object; resend the full patch each call"`
}

// validatePatchOutput is the structured result of validate_patch: whether the
// patch validated, the resolved preview, and the base revision to carry into a
// later write. On an invalid patch the handler returns the coded error instead, so
// the structured output is only populated on success.
type validatePatchOutput struct {
	// OK reports whether the cumulative patch validated cleanly under every
	// guardrail. It is true only when Preview is the resolved result; an invalid
	// patch yields a tool error (and {ok:false}) rather than a populated preview.
	OK bool `json:"ok" jsonschema:"true when the patch validated cleanly; false accompanies a coded tool error"`

	// Preview is the resolved tree the patch WOULD have produced, present only when
	// OK. It is typed `any` (raw resolved JSON) rather than the typed
	// service.ResolvedTree both because the resolved tree is recursive (a node's
	// children are nodes), which the SDK's reflective output-schema generator cannot
	// represent, and so the output schema leaves it unconstrained. Nothing is
	// persisted to produce it.
	Preview any `json:"preview,omitempty" jsonschema:"the resolved tree the patch would produce (raw JSON), present only when ok; nothing is persisted"`

	// BaseRevision is the document's CURRENT opaque revision token (service.Revision)
	// at validation time — the value the eventual write passes as its
	// optimistic-concurrency precondition (expectedRevision). Omitted when the wired
	// store does not support revisions.
	BaseRevision string `json:"baseRevision,omitempty" jsonschema:"the document's current opaque revision token, to pass as expectedRevision on the later write"`
}

// registerValidatePatch registers the validate_patch tool: it parses the input ops
// into a *service.Changeset (service.ParseChangeset), runs the dry-run primitive
// (service.DryRunPatch) — which loads, resolves, applies, and re-resolves under
// every guardrail but NEVER saves — and, on success, returns the resolved preview
// plus the document's current base revision. A parse failure (PATCH_*), an
// off-surface/ill-typed/structural changeset, or a re-resolution failure
// (STORAGE_NOT_FOUND, RESOLVE_*/SCHEMA_*/VAR_*) surfaces the *errors.CodedError
// verbatim as a tool error so the model can self-correct. The store is touched only
// by the dry-run's read-only Load and the revision lookup — never written.
func registerValidatePatch(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "validate_patch",
		Description: "Simulate an id-rooted RFC 6902 JSON Patch against a dashboard WITHOUT saving. This is the validate-only step of the propose-then-commit contract: it runs the full apply+re-resolve pipeline under every guardrail but NEVER persists — there is no save path through MCP. Resend the FULL cumulative patch each call (the server is stateless). On success returns {ok:true} with the resolved preview and the document's baseRevision; on failure returns {ok:false} plus the coded error (code + details) as a tool error so you can correct the ops and re-validate. To persist a validated patch, the human commits it via POST /api/patch, passing baseRevision as expectedRevision.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, input validatePatchInput) (*sdkmcp.CallToolResult, validatePatchOutput, error) {
		// Marshal the ops array back to RFC 6902 JSON Patch bytes for the parser, which
		// owns op-shape validation and surfaces PATCH_* coded errors on a malformed set.
		opsBytes, err := json.Marshal(input.Ops)
		if err != nil {
			return nil, validatePatchOutput{OK: false}, err
		}

		cs, err := svc.ParseChangeset(opsBytes)
		if err != nil {
			// Malformed patch bytes / invalid op set surface as PATCH_* verbatim.
			return nil, validatePatchOutput{OK: false}, err
		}

		// Dry-run: the SAME atomic pipeline a real Patch runs, MINUS the store write.
		// On any coded error nothing is persisted and the error surfaces verbatim so
		// the model can self-correct.
		result, err := svc.DryRunPatch(input.ID, cs)
		if err != nil {
			return nil, validatePatchOutput{OK: false}, err
		}

		out := validatePatchOutput{
			OK:      true,
			Preview: result.Resolved,
		}

		// Carry the document's CURRENT revision so the eventual write can pass it as
		// expectedRevision. A store that cannot report a revision fails with
		// STORAGE_CAPABILITY_UNSUPPORTED; the dry-run already succeeded and nothing is
		// persisted, so a missing-capability store still returns the valid preview with
		// an empty baseRevision rather than failing the whole validation.
		if rev, rerr := svc.Revision(input.ID); rerr == nil {
			out.BaseRevision = rev
		}

		return nil, out, nil
	})
}
