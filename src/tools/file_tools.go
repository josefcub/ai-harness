package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agent-project/harness/sandbox"
)

// RegisterFileTools registers all built-in file tools on the given registry.
func RegisterFileTools(reg *Registry) {
	workingDir := reg.workingDir

	// view
	reg.Register("view",
		"View a file, optionally returning a range of lines.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Relative path from working directory."},
				"start_line": map[string]interface{}{"type": "integer", "description": "0-based start line (optional)."},
				"end_line":   map[string]interface{}{"type": "integer", "description": "0-based end line, exclusive (optional)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolViewFile(workingDir, args)
		})

	// write
	reg.Register("write",
		"Create or overwrite a file in the working directory.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolCreateFile(workingDir, args)
		})

	// append
	reg.Register("append",
		"Appends text to the end of a file in the working directory. Creates the file if it doesn't exist.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolAppendToFile(workingDir, args)
		})

	// ls
	reg.Register("ls",
		"List files in a directory. Defaults to working directory if no path is given.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Relative path (optional; defaults to working directory)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolListFiles(workingDir, args)
		})

	// edit
	reg.Register("edit",
		"Edit a file by replacing the first occurrence of old_text with new_text.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":     map[string]interface{}{"type": "string"},
				"old_text": map[string]interface{}{"type": "string"},
				"new_text": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolEditFile(workingDir, args)
		})
}

func mustPath(args map[string]interface{}) (string, error) {
	p, ok := args["path"].(string)
	if !ok || p == "" {
		return "", fmt.Errorf("path is required")
	}
	return p, nil
}

func mustString(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}

func optInt(args map[string]interface{}, key string) (*int, error) {
	v, ok := args[key]
	if !ok {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok {
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	i := int(f)
	return &i, nil
}

// toolViewFile reads a file and returns its content, optionally limited to a line range.
func toolViewFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	startLine, err := optInt(args, "start_line")
	if err != nil {
		return "", err
	}
	endLine, err := optInt(args, "end_line")
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %v", err)
	}

	lines := strings.Split(string(data), "\n")

	// Handle trailing newline: if file ends with \n, Split produces an extra empty element
	if len(lines) > 0 && lines[len(lines)-1] == "" && strings.HasSuffix(string(data), "\n") {
		lines = lines[:len(lines)-1]
	}

	// Calculate start and end indices relative to the original list
	start := 0
	end := len(lines)
	if startLine != nil {
		s := *startLine
		if s < 0 {
			s = 0
		}
		if s > end {
			s = end
		}
		start = s
	}
	if endLine != nil {
		e := *endLine
		if e < 0 {
			e = 0
		}
		if e > end {
			e = end
		}
		end = e
	}

	if start > end {
		return "", fmt.Errorf("start_line (%d) must be less than or equal to end_line (%d)", start, end)
	}

	return strings.Join(lines[start:end], "\n"), nil
}

// toolCreateFile creates or overwrites a file.
func toolCreateFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	content, err := mustString(args, "content")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	// Create parent directories as needed
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return "", fmt.Errorf("create directories: %v", err)
	}

	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %v", err)
	}

	return fmt.Sprintf("File created: %s (%d bytes)", path, len(content)), nil
}

// toolAppendToFile appends content to a file.
func toolAppendToFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	content, err := mustString(args, "content")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open file: %v", err)
	}
	defer f.Close()

	n, err := f.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("write to file: %v", err)
	}

	return fmt.Sprintf("Appended %d bytes to: %s", n, path), nil
}

// toolListFiles lists files and directories in the given path.
func toolListFiles(workingDir string, args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("list directory: %v", err)
	}

	var results []string
	for _, entry := range entries {
		// Skip hidden files (starting with ".")
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info := ""
		if entry.IsDir() {
			info = "/"
		}
		results = append(results, entry.Name()+info)
	}

	return strings.Join(results, "\n"), nil
}

// toolEditFile replaces the first occurrence of old_text with new_text in a file.
func toolEditFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	oldText, err := mustString(args, "old_text")
	if err != nil {
		return "", err
	}

	newText, err := mustString(args, "new_text")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %v", err)
	}

	content := string(data)
	idx := strings.Index(content, oldText)
	if idx == -1 {
		return "", fmt.Errorf("old_text not found in file")
	}

	// Replace only the first occurrence
	content = content[:idx] + newText + content[idx+len(oldText):]

	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %v", err)
	}

	return fmt.Sprintf("Edited %s: replaced %d bytes with %d bytes", path, len(oldText), len(newText)), nil
}
