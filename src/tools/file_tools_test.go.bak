package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "file-tools-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	reg := New(dir)
	RegisterFileTools(reg)
	RegisterGrepTools(reg)
	RegisterGlobTools(reg)
	return reg, dir
}

func TestViewFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a test file
	content := "line one\nline two\nline three\n"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("view", `{"path":"test.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	if lines[0] != "line one" {
		t.Errorf("expected 'line one', got %q", lines[0])
	}
}

func TestViewFile_LineRange(t *testing.T) {
	reg, dir := newTestRegistry(t)

	content := "alpha\nbeta\ngamma\ndelta\n"
	path := filepath.Join(dir, "range.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Get lines 1-3 (beta and gamma)
	result, err := reg.Dispatch("view", `{"path":"range.txt","start_line":1,"end_line":3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "beta" {
		t.Errorf("expected 'beta', got %q", lines[0])
	}
	if lines[1] != "gamma" {
		t.Errorf("expected 'gamma', got %q", lines[1])
	}
}

func TestViewFile_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("view", `{"path":"/etc/passwd"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}

func TestViewFile_Traversal(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("view", `{"path":"../../../etc/passwd"}`)
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestCreateFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	result, err := reg.Dispatch("write", `{"path":"new.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "File created") {
		t.Errorf("expected 'File created' in result, got: %s", result)
	}

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestCreateFile_NestedDir(t *testing.T) {
	reg, dir := newTestRegistry(t)

	result, err := reg.Dispatch("write", `{"path":"sub/nested/file.txt","content":"nested content"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "File created") {
		t.Errorf("expected 'File created' in result, got: %s", result)
	}

	// Verify nested file was created
	data, err := os.ReadFile(filepath.Join(dir, "sub", "nested", "file.txt"))
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("expected 'nested content', got %q", string(data))
	}
}

func TestAppendToFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create initial file
	path := filepath.Join(dir, "append.txt")
	if err := os.WriteFile(path, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("append", `{"path":"append.txt","content":"appended"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Appended") {
		t.Errorf("expected 'Appended' in result, got: %s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "initial\nappended" {
		t.Errorf("expected 'initial\\nappended', got %q", string(data))
	}
}

func TestListFiles(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create some files
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	result, err := reg.Dispatch("ls", `{"path":"."}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 entries, got %d: %v", len(lines), lines)
	}

	// Check for expected entries
	found := make(map[string]bool)
	for _, line := range lines {
		found[line] = true
	}
	if !found["a.txt"] {
		t.Error("expected 'a.txt' in listing")
	}
	if !found["b.txt"] {
		t.Error("expected 'b.txt' in listing")
	}
	if !found["subdir/"] {
		t.Error("expected 'subdir/' in listing")
	}
}

func TestEditFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a test file
	content := "foo bar baz\nqux quux\n"
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("edit", `{"path":"edit.txt","old_text":"bar","new_text":"BAR"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Edited") {
		t.Errorf("expected 'Edited' in result, got: %s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foo BAR baz\nqux quux\n" {
		t.Errorf("expected 'foo BAR baz\\nqux quux\\n', got %q", string(data))
	}
}

func TestEditFile_NotFound(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "edit2.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := reg.Dispatch("edit", `{"path":"edit2.txt","old_text":"not here","new_text":"replacement"}`)
	if err == nil {
		t.Fatal("expected error for text not found")
	}
}

func TestViewFile_EmptyFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0644)

	result, err := reg.Dispatch("view", `{"path":"empty.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCreateFile_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("write", `{"path":"/tmp/escape.txt","content":"data"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}

func TestListFiles_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("ls", `{"path":"../"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}
