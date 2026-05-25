package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agent-project/harness/sandbox"
)

// RegisterGrepTools registers the grep tool on the given registry.
func RegisterGrepTools(reg *Registry) {
	workingDir := reg.workingDir

	reg.Register("grep",
		"Search for a pattern in files. Supports plain text and regex.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern":   map[string]interface{}{"type": "string"},
				"path":      map[string]interface{}{"type": "string", "description": "File or directory to search (relative)."},
				"use_regex": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolGrep(workingDir, args)
		})
}

// toolGrep searches for a pattern in files or directories.
func toolGrep(workingDir string, args map[string]interface{}) (string, error) {
	pattern, err := mustString(args, "pattern")
	if err != nil {
		return "", err
	}

	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	useRegex := false
	if v, ok := args["use_regex"]; ok {
		if b, ok := v.(bool); ok {
			useRegex = b
		}
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat: %v", err)
	}

	var results []string

	if info.IsDir() {
		// Walk directory without following symlinks.
		// filepath.WalkDir does not follow symlinks, preventing
		// symlink-based escapes during directory traversal.
		err := filepath.WalkDir(resolved, func(fp string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			matches, err := grepFile(fp, pattern, useRegex, workingDir)
			if err != nil {
				return nil // Skip files we can't read
			}
			results = append(results, matches...)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk directory: %v", err)
		}
	} else {
		matches, err := grepFile(resolved, pattern, useRegex, workingDir)
		if err != nil {
			return "", err
		}
		results = append(results, matches...)
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}

	return strings.Join(results, "\n"), nil
}

// grepFile searches for a pattern in a single file and returns matched lines.
func grepFile(filePath, pattern string, useRegex bool, workingDir string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []string

	// Get relative path from workingDir
	relPath, err := filepath.Rel(workingDir, filePath)
	if err != nil {
		relPath = filePath
	}

	if useRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %v", pattern, err)
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
	} else {
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(line, pattern) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
	}

	return matches, nil
}
