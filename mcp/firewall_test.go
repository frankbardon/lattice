package mcp_test

import (
	"os/exec"
	"strings"
	"testing"
)

// sdkImportPath is the MCP SDK module the mcp core must never depend on. The
// whole point of this package is to define tools without coupling to a protocol
// SDK version; the coupling lives only in the mcp/gosdk adapter.
const sdkImportPath = "github.com/modelcontextprotocol/go-sdk"

// corePackage is the import path whose transitive dependency set is firewalled.
const corePackage = "github.com/frankbardon/lattice/mcp"

// TestImportFirewall asserts the mcp core package's transitive import set does
// NOT include the MCP SDK. This is the contract that makes the SDK-free insulation
// real: if a future edit imports the SDK (directly or through a helper), `go list
// -deps` surfaces it and this test fails. It shells out to the go tool and skips
// gracefully when the go binary is unavailable (so it never blocks an SDK-less
// CI runner).
func TestImportFirewall(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not on PATH; skipping import-firewall check")
	}

	out, err := exec.Command(goBin, "list", "-deps", corePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", corePackage, err, out)
	}

	for _, dep := range strings.Fields(string(out)) {
		if dep == sdkImportPath || strings.HasPrefix(dep, sdkImportPath+"/") {
			t.Fatalf("import firewall breached: %s transitively imports %s (via %s); the SDK coupling must stay in mcp/gosdk only", corePackage, sdkImportPath, dep)
		}
	}
}
