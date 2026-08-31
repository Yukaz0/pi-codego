package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pi/pkg/types"
)

// GlobTool finds files by glob pattern, walking the working directory.
type GlobTool struct{}

func NewGlobTool() *GlobTool { return &GlobTool{} }

func (g *GlobTool) Name() string { return "glob" }

func (g *GlobTool) Description() string {
	return "Find files matching a glob pattern (e.g. '**/*.go', 'pkg/**/*.json'). Returns paths sorted by modification time, newest first."
}

func (g *GlobTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        g.Name(),
		Description: g.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"pattern": {Type: "string", Description: "Glob pattern to match against file paths"},
				"path":    {Type: "string", Description: "Base directory to search in (defaults to current working directory)"},
			},
			Required: []string{"pattern"},
		},
	}
}

const globMaxResults = 200

var globSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, "__pycache__": true, ".venv": true,
}

func (g *GlobTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", fmt.Errorf("pattern is required")
	}
	base := args.Path
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = cwd
	}
	pattern := filepath.Clean(args.Pattern)

	var matches []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if globSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			return nil
		}
		if globMatch(pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sortByMTime(base, matches)
	if len(matches) > globMaxResults {
		out := strings.Join(matches[:globMaxResults], "\n")
		return fmt.Sprintf("%s\n... (%d more files truncated)", out, len(matches)-globMaxResults), nil
	}
	if len(matches) == 0 {
		return "No files matched pattern.", nil
	}
	return strings.Join(matches, "\n"), nil
}

// sortByMTime sorts matched relative paths by modification time, newest first.
func sortByMTime(base string, rels []string) {
	mtime := make(map[string]int64, len(rels))
	for _, r := range rels {
		if fi, err := os.Stat(filepath.Join(base, r)); err == nil {
			mtime[r] = fi.ModTime().UnixNano()
		}
	}
	sort.SliceStable(rels, func(i, j int) bool { return mtime[rels[i]] > mtime[rels[j]] })
}

// globMatch supports '**' (any dirs), '*' (within segment), '?' (single char).
func globMatch(pattern, name string) bool {
	patSegs := strings.Split(pattern, "/")
	namSegs := strings.Split(name, "/")
	return globMatchSegs(patSegs, namSegs)
}

func globMatchSegs(pat, nam []string) bool {
	if len(pat) == 0 {
		return len(nam) == 0
	}
	if pat[0] == "**" {
		// ** matches zero or more path segments
		for i := 0; i <= len(nam); i++ {
			if globMatchSegs(pat[1:], nam[i:]) {
				return true
			}
		}
		return false
	}
	if len(nam) == 0 {
		return false
	}
	if !segmentMatch(pat[0], nam[0]) {
		return false
	}
	return globMatchSegs(pat[1:], nam[1:])
}

func segmentMatch(pattern, s string) bool {
	// simple wildcard matcher for one path segment
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// try matching rest at every position
			for i := 0; i <= len(s); i++ {
				if segmentMatch(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			pattern, s = pattern[1:], s[1:]
		default:
			if len(s) == 0 || s[0] != pattern[0] {
				return false
			}
			pattern, s = pattern[1:], s[1:]
		}
	}
	return len(s) == 0
}
