package wire

import (
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

func TestMCPAccessIsOpaqueInsideClientRequestResult(t *testing.T) {
	source, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	const bearer = "nested-wire-secret"
	access, err := (mcp.Access{}).WithBearer(source, bearer)
	if err != nil {
		t.Fatal(err)
	}
	value := ClientRequestResult{MCPAccess: access}
	for _, formatted := range []string{
		fmt.Sprintf("%v", value),
		fmt.Sprintf("%+v", value),
		fmt.Sprintf("%#v", value),
	} {
		if strings.Contains(formatted, bearer) {
			t.Fatalf("ClientRequestResult exposed bearer as %q", formatted)
		}
	}
}
