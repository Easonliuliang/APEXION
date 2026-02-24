package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractGo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	code := `package sample

type Server struct {
	Name string
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Start() error {
	return nil
}

func helper() {}
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	sigs, err := extractGo(path)
	if err != nil {
		t.Fatal(err)
	}
	if sigs.Language != "go" {
		t.Fatalf("expected language go, got %s", sigs.Language)
	}
	if len(sigs.Types) != 1 || sigs.Types[0].Name != "Server" {
		t.Fatalf("expected 1 type 'Server', got %v", sigs.Types)
	}
	if len(sigs.Functions) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(sigs.Functions))
	}

	names := map[string]bool{}
	for _, f := range sigs.Functions {
		names[f.Name] = true
	}
	for _, want := range []string{"NewServer", "Start", "helper"} {
		if !names[want] {
			t.Errorf("missing function %s", want)
		}
	}
}

func TestExtractGoExported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exported.go")
	code := `package exported

type PublicType struct{}
type privateType struct{}

func PublicFunc() {}
func privateFunc() {}
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	sigs, err := extractGo(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, tp := range sigs.Types {
		switch tp.Name {
		case "PublicType":
			if !tp.Exported {
				t.Error("PublicType should be exported")
			}
		case "privateType":
			if tp.Exported {
				t.Error("privateType should not be exported")
			}
		}
	}

	for _, fn := range sigs.Functions {
		switch fn.Name {
		case "PublicFunc":
			if !fn.Exported {
				t.Error("PublicFunc should be exported")
			}
		case "privateFunc":
			if fn.Exported {
				t.Error("privateFunc should not be exported")
			}
		}
	}
}

func TestExtractGoSignatureFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sig.go")
	code := `package sig

func Add(a, b int) int { return a + b }
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	sigs, err := extractGo(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(sigs.Functions))
	}
	sig := sigs.Functions[0].Signature
	if !strings.Contains(sig, "func Add(") {
		t.Errorf("signature should contain 'func Add(', got %q", sig)
	}
	if !strings.Contains(sig, "int") {
		t.Errorf("signature should contain return type, got %q", sig)
	}
}

func TestExtractGenericPython(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.py")
	code := `class MyClass:
    def __init__(self):
        pass

def public_func(a, b):
    return a + b

def _private_func():
    pass
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	sigs, err := extractGeneric(path, ".py")
	if err != nil {
		t.Fatal(err)
	}
	if sigs.Language != "py" {
		t.Fatalf("expected language py, got %s", sigs.Language)
	}
	if len(sigs.Types) != 1 || sigs.Types[0].Name != "MyClass" {
		t.Fatalf("expected 1 type MyClass, got %v", sigs.Types)
	}
	if len(sigs.Functions) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(sigs.Functions))
	}

	// Check _private_func is not exported
	for _, f := range sigs.Functions {
		if f.Name == "_private_func" && f.Exported {
			t.Error("_private_func should not be exported")
		}
		if f.Name == "public_func" && !f.Exported {
			t.Error("public_func should be exported")
		}
	}
}

func TestExtractGenericTS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.ts")
	code := `export function fetchData(url: string): Promise<any> {
    return fetch(url);
}

export class ApiClient {
    constructor(private baseUrl: string) {}
}

function helperFunc() {}

export interface Config {
    timeout: number;
}
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	sigs, err := extractGeneric(path, ".ts")
	if err != nil {
		t.Fatal(err)
	}
	if sigs.Language != "ts" {
		t.Fatalf("expected language ts, got %s", sigs.Language)
	}

	// Check exported vs non-exported functions
	var exportedFuncs, unexportedFuncs int
	for _, f := range sigs.Functions {
		if f.Exported {
			exportedFuncs++
		} else {
			unexportedFuncs++
		}
	}
	if exportedFuncs < 1 {
		t.Error("expected at least 1 exported function")
	}
	if unexportedFuncs < 1 {
		t.Error("expected at least 1 non-exported function (helperFunc)")
	}

	// Should have types: class + interface
	if len(sigs.Types) < 2 {
		t.Fatalf("expected at least 2 types (class + interface), got %d", len(sigs.Types))
	}
}

func TestBuildAndRender(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

type App struct{}

func NewApp() *App { return &App{} }
func (a *App) Run() error { return nil }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0644); err != nil {
		t.Fatal(err)
	}

	pyCode := `class Server:
    def start(self):
        pass

def create_server():
    return Server()
`
	if err := os.WriteFile(filepath.Join(dir, "server.py"), []byte(pyCode), 0644); err != nil {
		t.Fatal(err)
	}

	rm := New(dir, 4096, nil)
	if rm.IsBuilt() {
		t.Fatal("should not be built yet")
	}

	if err := rm.Build(); err != nil {
		t.Fatal(err)
	}
	if !rm.IsBuilt() {
		t.Fatal("should be built after Build()")
	}
	if rm.FileCount() != 2 {
		t.Fatalf("expected 2 files, got %d", rm.FileCount())
	}
	if rm.SymbolCount() == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	output := rm.Render(0)
	if output == "" {
		t.Fatal("render output should not be empty")
	}
	if !strings.Contains(output, "main.go") {
		t.Error("render should contain main.go")
	}
	if !strings.Contains(output, "server.py") {
		t.Error("render should contain server.py")
	}
}

func TestBuildEmpty(t *testing.T) {
	dir := t.TempDir()
	rm := New(dir, 4096, nil)
	if err := rm.Build(); err != nil {
		t.Fatal(err)
	}
	if rm.FileCount() != 0 {
		t.Fatalf("expected 0 files, got %d", rm.FileCount())
	}
	if output := rm.Render(0); output != "" {
		t.Fatalf("expected empty render, got %q", output)
	}
}

func TestExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc Main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "generated.go"),
		[]byte("package main\n\nfunc Generated() {}\n"), 0644)

	rm := New(dir, 4096, []string{"generated.go"})
	if err := rm.Build(); err != nil {
		t.Fatal(err)
	}

	if rm.FileCount() != 1 {
		t.Fatalf("expected 1 file after exclude, got %d", rm.FileCount())
	}
	output := rm.Render(0)
	if strings.Contains(output, "generated.go") {
		t.Error("render should not contain excluded file")
	}
	if !strings.Contains(output, "main.go") {
		t.Error("render should contain main.go")
	}
}

func TestBuildSkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a small valid Go file
	os.WriteFile(filepath.Join(dir, "small.go"),
		[]byte("package small\n\nfunc Small() {}\n"), 0644)

	// Create a file > 512KB — should be skipped
	big := make([]byte, 600*1024)
	copy(big, []byte("package big\n\nfunc Big() {}\n"))
	os.WriteFile(filepath.Join(dir, "big.go"), big, 0644)

	rm := New(dir, 4096, nil)
	rm.Build()
	if rm.FileCount() != 1 {
		t.Fatalf("expected 1 file (skipping large), got %d", rm.FileCount())
	}
}

func TestBuildSkipsVendor(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc Main() {}\n"), 0644)

	vendorDir := filepath.Join(dir, "vendor")
	os.MkdirAll(vendorDir, 0755)
	os.WriteFile(filepath.Join(vendorDir, "lib.go"),
		[]byte("package lib\n\nfunc Lib() {}\n"), 0644)

	rm := New(dir, 4096, nil)
	rm.Build()
	if rm.FileCount() != 1 {
		t.Fatalf("expected 1 file (skipping vendor), got %d", rm.FileCount())
	}
}

func TestNewDefaults(t *testing.T) {
	rm := New("/tmp", 0, nil)
	if rm.maxTokens != 4096 {
		t.Fatalf("expected default maxTokens 4096, got %d", rm.maxTokens)
	}
}
