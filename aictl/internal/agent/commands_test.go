package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFront string
		wantBody  string
		wantErr   bool
	}{
		{
			name:      "normal",
			input:     "---\nname: review\n---\nReview this code.",
			wantFront: "name: review",
			wantBody:  "Review this code.",
		},
		{
			name:      "no frontmatter",
			input:     "Just a body with no YAML.",
			wantFront: "",
			wantBody:  "Just a body with no YAML.",
		},
		{
			name:    "unclosed frontmatter",
			input:   "---\nname: broken\nNo closing dashes.",
			wantErr: true,
		},
		{
			name:      "empty body",
			input:     "---\nname: test\n---\n",
			wantFront: "name: test",
			wantBody:  "",
		},
		{
			name:      "multiline frontmatter",
			input:     "---\nname: review\ndescription: Code review\nargs:\n  - name: file\n    required: true\n---\nReview {{.file}}.",
			wantFront: "name: review\ndescription: Code review\nargs:\n  - name: file\n    required: true",
			wantBody:  "Review {{.file}}.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			front, body, err := splitFrontmatter(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if front != tt.wantFront {
				t.Errorf("front = %q, want %q", front, tt.wantFront)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestParseCommandFile(t *testing.T) {
	dir := t.TempDir()

	// Write a valid command file.
	content := `---
name: review
description: Code review
args:
  - name: file
    required: true
  - name: focus
    required: false
    default: all
---
Review {{.file}} focusing on {{.focus}}.`

	path := filepath.Join(dir, "review.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parseCommandFile: %v", err)
	}

	if cmd.Name != "review" {
		t.Errorf("Name = %q, want %q", cmd.Name, "review")
	}
	if cmd.Description != "Code review" {
		t.Errorf("Description = %q, want %q", cmd.Description, "Code review")
	}
	if len(cmd.Args) != 2 {
		t.Fatalf("len(Args) = %d, want 2", len(cmd.Args))
	}
	if cmd.Args[0].Name != "file" || !cmd.Args[0].Required {
		t.Errorf("Args[0] = %+v, want file/required", cmd.Args[0])
	}
	if cmd.Args[1].Default != "all" {
		t.Errorf("Args[1].Default = %q, want %q", cmd.Args[1].Default, "all")
	}
}

func TestParseCommandFile_NameFromFilename(t *testing.T) {
	dir := t.TempDir()

	content := `---
description: Deploy helper
---
Deploy the application.`

	path := filepath.Join(dir, "deploy.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parseCommandFile: %v", err)
	}

	if cmd.Name != "deploy" {
		t.Errorf("Name = %q, want %q (derived from filename)", cmd.Name, "deploy")
	}
}

func TestRenderCommand(t *testing.T) {
	cmd := &CustomCommand{
		Name:        "review",
		Description: "Code review",
		Args: []CommandArg{
			{Name: "file", Required: true},
			{Name: "focus", Default: "all"},
		},
		body: "Review {{.file}} focusing on {{.focus}}.",
	}

	tests := []struct {
		name    string
		rawArgs string
		want    string
	}{
		{
			name:    "all args provided",
			rawArgs: "main.go security",
			want:    "Review main.go focusing on security.",
		},
		{
			name:    "default used",
			rawArgs: "main.go",
			want:    "Review main.go focusing on all.",
		},
		{
			name:    "no args",
			rawArgs: "",
			want:    "Review  focusing on all.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCommand(cmd, tt.rawArgs)
			if err != nil {
				t.Fatalf("renderCommand: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderCommand_LastArgGetsRemainder(t *testing.T) {
	cmd := &CustomCommand{
		Name: "ask",
		Args: []CommandArg{
			{Name: "question"},
		},
		body: "Answer: {{.question}}",
	}

	got, err := renderCommand(cmd, "what is the meaning of life")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Answer: what is the meaning of life" {
		t.Errorf("got %q, want last arg to capture remainder", got)
	}
}

func TestBuildArgMap(t *testing.T) {
	cmd := &CustomCommand{
		Args: []CommandArg{
			{Name: "a"},
			{Name: "b", Default: "default_b"},
		},
	}

	data := buildArgMap(cmd, "hello")
	if data["a"] != "hello" {
		t.Errorf("a = %q, want %q", data["a"], "hello")
	}
	if data["b"] != "default_b" {
		t.Errorf("b = %q, want %q (default)", data["b"], "default_b")
	}
	if data["_args"] != "hello" {
		t.Errorf("_args = %q, want %q", data["_args"], "hello")
	}
}

func TestLoadCustomCommands(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, ".aictl", "commands")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write two command files.
	if err := os.WriteFile(filepath.Join(cmdDir, "review.md"), []byte(`---
name: review
description: Review code
args:
  - name: file
    required: true
---
Review {{.file}}.`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cmdDir, "test.md"), []byte(`---
name: test
description: Run tests
---
Run all tests.`), 0644); err != nil {
		t.Fatal(err)
	}

	// Non-.md file should be ignored.
	if err := os.WriteFile(filepath.Join(cmdDir, "notes.txt"), []byte("not a command"), 0644); err != nil {
		t.Fatal(err)
	}

	commands := loadCustomCommands(dir)
	if len(commands) != 2 {
		t.Fatalf("len(commands) = %d, want 2", len(commands))
	}
	if commands["review"] == nil {
		t.Error("missing 'review' command")
	}
	if commands["test"] == nil {
		t.Error("missing 'test' command")
	}
}

func TestFormatCommandList(t *testing.T) {
	commands := map[string]*CustomCommand{
		"review": {
			Name:        "review",
			Description: "Code review",
			Args:        []CommandArg{{Name: "file", Required: true}},
		},
		"deploy": {
			Name:        "deploy",
			Description: "Deploy app",
		},
	}

	out := formatCommandList(commands)
	if !strings.Contains(out, "Custom commands (2)") {
		t.Errorf("should show count, got: %s", out)
	}
	if !strings.Contains(out, "/review <file>") {
		t.Errorf("should show required arg, got: %s", out)
	}
	if !strings.Contains(out, "/deploy") {
		t.Errorf("should show deploy, got: %s", out)
	}
}

func TestFormatCommandList_Empty(t *testing.T) {
	out := formatCommandList(nil)
	if !strings.Contains(out, "No custom commands found") {
		t.Errorf("expected 'No custom commands found', got: %s", out)
	}
}

func TestCommandDirs(t *testing.T) {
	dirs := commandDirs("/project", "/project")
	// When cwd == gitRoot, should not duplicate.
	count := 0
	for _, d := range dirs {
		if strings.Contains(d, ".aictl/commands") {
			count++
		}
	}
	// Should have exactly one .aictl/commands entry (from cwd), not two.
	if count != 1 {
		t.Errorf("expected 1 .aictl/commands dir when cwd==gitRoot, got %d: %v", count, dirs)
	}
}

func TestParseMemoryInput(t *testing.T) {
	tests := []struct {
		input       string
		wantContent string
		wantTags    []string
	}{
		{"prefer snake_case #preference #style", "prefer snake_case", []string{"preference", "style"}},
		{"no tags here", "no tags here", nil},
		{"#onlytag", "", []string{"onlytag"}},
		{"mixed #tag1 content #tag2", "mixed content", []string{"tag1", "tag2"}},
		{"", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			content, tags := parseMemoryInput(tt.input)
			if content != tt.wantContent {
				t.Errorf("content = %q, want %q", content, tt.wantContent)
			}
			if len(tags) != len(tt.wantTags) {
				t.Errorf("tags = %v, want %v", tags, tt.wantTags)
			}
		})
	}
}

func TestCommandDirs_DifferentRoots(t *testing.T) {
	dirs := commandDirs("/project/sub", "/project")
	// Should have both gitRoot and cwd entries.
	found := 0
	for _, d := range dirs {
		if strings.Contains(d, ".aictl/commands") {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected at least 2 .aictl/commands dirs when cwd!=gitRoot, got %d: %v", found, dirs)
	}
}
