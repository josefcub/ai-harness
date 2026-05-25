package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_PlainText(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create test file
	content := "hello world\nfoo bar\nhello again\n"
	path := filepath.Join(dir, "search.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("grep", `{"pattern":"hello","path":"search.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	// Should match 2 lines (line 1 and line 3)
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(lines), lines)
	}

	// First match should be on line 1
	if !strings.Contains(lines[0], "search.txt:1:hello world") {
		t.Errorf("unexpected first match: %s", lines[0])
	}
}

func TestGrep_Regex(t *testing.T) {
	reg, dir := newTestRegistry(t)

	content := "foo123bar\nhello\nfoo456world\n"
	path := filepath.Join(dir, "regex.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("grep", `{"pattern":"foo\\d+","path":"regex.txt","use_regex":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 regex matches, got %d: %v", len(lines), lines)
	}
}

func TestGrep_Directory(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create files in a subdirectory
	sub := filepath.Join(dir, "src")
	os.Mkdir(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("needle in haystack\nother line\n"), 0644)
	os.WriteFile(filepath.Join(sub, "b.txt"), []byte("no match here\nneedle again\n"), 0644)

	result, err := reg.Dispatch("grep", `{"pattern":"needle","path":"src"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 matches total (one in each file)
	if !strings.Contains(result, "needle in haystack") {
		t.Error("expected match in a.txt")
	}
	if !strings.Contains(result, "needle again") {
		t.Error("expected match in b.txt")
	}
}

func TestGrep_NoMatches(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "nomatch.txt")
	os.WriteFile(path, []byte("no matches here\n"), 0644)

	result, err := reg.Dispatch("grep", `{"pattern":"xyz","path":"nomatch.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", result)
	}
}

func TestGrep_SymlinkNoFollow(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a file outside the sandbox
	outside := filepath.Join(filepath.Dir(dir), "outside-"+filepath.Base(dir))
	os.MkdirAll(outside, 0755)
	t.Cleanup(func() { os.RemoveAll(outside) })
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("sensitive data\n"), 0644)

	// Create a symlink inside the sandbox pointing outside
	linkDir := filepath.Join(dir, "evil_link")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skip("symlinks not supported")
	}

	// Grep on the symlinked directory should NOT follow the symlink
	// (WalkDir skips symlinks, so it won't traverse into it)
	result, err := reg.Dispatch("grep", `{"pattern":"sensitive","path":"evil_link"}`)
	if err != nil {
		// Error is also acceptable (stat may fail on broken symlink)
		return
	}
	// If we got a result, it must not contain the external data
	if strings.Contains(result, "sensitive data") {
		t.Error("grep followed symlink outside sandbox")
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("test"), 0644)

	_, err := reg.Dispatch("grep", `{"pattern":"[invalid","path":"test.txt","use_regex":true}`)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
