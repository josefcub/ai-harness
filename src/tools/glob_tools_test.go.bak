package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlob_SingleLevel(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo"), 0644)
	os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package bar"), 0644)
	os.WriteFile(filepath.Join(dir, "baz.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hidden"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(lines), lines)
	}
	if lines[0] != "foo.go" && lines[1] != "foo.go" {
		t.Errorf("expected foo.go in results: %v", lines)
	}
	if lines[0] != "bar.go" && lines[1] != "bar.go" {
		t.Errorf("expected bar.go in results: %v", lines)
	}

	// .hidden should not appear
	if strings.Contains(result, ".hidden") {
		t.Error("hidden file should not be included")
	}
}

func TestGlob_Recursive(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "foo.go"), []byte("package foo"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "bar.go"), []byte("package bar"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "readme.txt"), []byte("readme"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(lines), lines)
	}

	// Check all .go files are present
	found := make(map[string]bool)
	for _, line := range lines {
		found[line] = true
	}
	if !found["main.go"] {
		t.Error("expected main.go in results")
	}
	if !found["src/foo.go"] {
		t.Error("expected src/foo.go in results")
	}
	if !found["src/pkg/bar.go"] {
		t.Error("expected src/pkg/bar.go in results")
	}
}

func TestGlob_WithPath(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "b.go"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "c.txt"), []byte("c"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go","path":"src/pkg"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(lines), lines)
	}
	if lines[0] != "src/pkg/b.go" {
		t.Errorf("expected 'src/pkg/b.go', got %q", lines[0])
	}
}

func TestGlob_NoMatches(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "only.txt"), []byte("text"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", result)
	}
}

func TestGlob_NotADirectory(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("text"), 0644)

	_, err := reg.Dispatch("glob", `{"pattern":"*","path":"file.txt"}`)
	if err == nil {
		t.Fatal("expected error when path is not a directory")
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("glob", `{"path":"."}`)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}
