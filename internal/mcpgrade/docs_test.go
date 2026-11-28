package mcpgrade

import (
	"os"
	"strings"
	"testing"
)

func TestDocsListEveryCheck(t *testing.T) {
	doc, err := os.ReadFile("../../docs/mcp-server.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if !strings.Contains(string(doc), "`"+c.id+"`") {
			t.Errorf("docs/mcp-server.md does not explain %s; add it to the %s table", c.id, c.category())
		}
	}
}
