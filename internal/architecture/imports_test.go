// Package architecture checks the import graph of the inner application layers.
package architecture

import (
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func forbidden(layer, imported string) bool {
	if strings.Contains(imported, "/internal/adapters") || strings.Contains(imported, "/internal/config") || strings.Contains(imported, "/migrations") {
		return true
	}
	if layer == "domain" && strings.Contains(imported, "/internal/usecase") {
		return true
	}
	for _, prefix := range []string{"net", "os", "io/fs", "path/filepath", "embed", "database/sql", "github.com/jackc/", "github.com/coder/websocket", "github.com/ollama/", "github.com/pressly/", "golang.org/x/crypto"} {
		if imported == prefix || strings.HasPrefix(imported, prefix+"/") || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(imported, prefix)) {
			return true
		}
	}
	return false
}

func TestImportRulesCatchInfrastructure(t *testing.T) {
	for _, tc := range []struct {
		layer, imported string
		want            bool
	}{
		{"domain", "net/http", true}, {"usecase", "os", true}, {"usecase", "io/fs", true},
		{"usecase", "github.com/jackc/pgx/v5", true}, {"usecase", "github.com/coder/websocket", true},
		{"domain", "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/lifecycle", true},
		{"usecase", "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/postgres", true},
		{"usecase", "context", false}, {"domain", "time", false},
		{"usecase", "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player", false},
	} {
		if got := forbidden(tc.layer, tc.imported); got != tc.want {
			t.Errorf("%s -> %s: forbidden=%v", tc.layer, tc.imported, got)
		}
	}
}

func TestInnerLayerImportGraph(t *testing.T) {
	command := exec.CommandContext(t.Context(), "go", "list", "-json", "../...")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	checked := 0
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		layer := ""
		if strings.Contains(pkg.ImportPath, "/internal/domain/") {
			layer = "domain"
		}
		if strings.Contains(pkg.ImportPath, "/internal/usecase/") {
			layer = "usecase"
		}
		if layer == "" {
			continue
		}
		checked++
		for _, imported := range pkg.Imports {
			if forbidden(layer, imported) {
				t.Errorf("forbidden import: %s -> %s", pkg.ImportPath, imported)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no inner-layer packages checked")
	}
}
