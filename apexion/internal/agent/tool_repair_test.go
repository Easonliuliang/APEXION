package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

func TestRepairToolNameAlias(t *testing.T) {
	reg := tools.DefaultRegistry(nil, nil)
	got, ok := repairToolName("webfetch", reg)
	if !ok {
		t.Fatal("expected alias repair")
	}
	if got != "web_fetch" {
		t.Fatalf("expected web_fetch, got %q", got)
	}
}

func TestRepairToolArgsReadFile(t *testing.T) {
	raw := json.RawMessage(`{"path":"/tmp/a.txt"}`)
	fixed, changed := repairToolArgs("read_file", raw)
	if !changed {
		t.Fatal("expected args to be changed")
	}
	if !strings.Contains(string(fixed), `"file_path"`) {
		t.Fatalf("expected file_path in repaired args: %s", string(fixed))
	}
}

func TestRepairToolArgsDocContext(t *testing.T) {
	raw := json.RawMessage(`{"query":"gorm preload","framework":"gorm","ver":"v2"}`)
	fixed, changed := repairToolArgs("doc_context", raw)
	if !changed {
		t.Fatal("expected args to be changed")
	}
	s := string(fixed)
	for _, key := range []string{`"topic"`, `"library"`, `"version"`} {
		if !strings.Contains(s, key) {
			t.Fatalf("expected %s in repaired args: %s", key, s)
		}
	}
}

func TestExecuteToolWithRepairUnknownName(t *testing.T) {
	reg := tools.DefaultRegistry(nil, nil)
	exec := tools.NewExecutor(reg, permission.AllowAllPolicy{})
	a := &Agent{
		executor: exec,
		config:   config.DefaultConfig(),
		io:       tui.NewBufferIO(),
	}
	call := &provider.ToolCallRequest{
		ID:    "t1",
		Name:  "ls",
		Input: json.RawMessage(`{"path":"."}`),
	}
	res, executedName, notes := a.executeToolWithRepair(context.Background(), call)
	if executedName != "list_dir" {
		t.Fatalf("expected executed tool list_dir, got %q", executedName)
	}
	if len(notes) == 0 {
		t.Fatal("expected repair notes")
	}
	if res.IsError {
		t.Fatalf("expected successful repaired execution, got: %s", res.Content)
	}
	if strings.Contains(strings.ToLower(res.Content), "unknown tool") {
		t.Fatalf("expected repaired execution, got content: %s", res.Content)
	}
}
