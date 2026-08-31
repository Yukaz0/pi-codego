package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pi/pkg/types"
)

// GrepTool searches file contents with a regex pattern.
type GrepTool struct{}

func NewGrepTool() *GrepTool { return &GrepTool{} }

func (g *GrepTool) Name() string { return "grep" }

func (g *GrepTool) Description() string {
	return "Search for a regex pattern inside files under a directory. Returns matching lines as path:line:text, skipping binary files and common ignore directories."
}

func (g *GrepTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        g.Name(),
		Description: g.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"pattern":   {Type: "string", Description: "Regular expression to search for (Go regexp syntax)"},
				"path":      {Type: "string", Description: "Directory or file to search (defaults to current working directory)"},
				"file_glob": {Type: "string", Description: "Optional glob to filter file names, e.g. '*.go'"},
			},
			Required: []string{"pattern"},
		},
	}
}

const (
	grepMaxResults  = 200
	grepMaxFileSize = 2 << 20 // 2 MiB per file
)

func (g *GrepTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
		FileGlob string `json:"file_glob"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", fmt.Errorf("pattern is required")
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	root := args.Path
	if root == "" {
		cwd, cerr := os.Getwd()
		if cerr != nil {
			return "", cerr
		}
		root = cwd
	}

	var out []string
	var filesScanned int

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(out) >= grepMaxResults {
			return fs.SkipAll
		}
		if d.IsDir() {
			if globSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if args.FileGlob != "" {
			if ok, _ := filepath.Match(args.FileGlob, d.Name()); !ok {
				return nil
			}
		}
		fi, serr := d.Info()
		if serr != nil || fi.Size() > grepMaxFileSize {
			return nil
		}
		matches, isBinary := grepFile(path, re)
		if isBinary || len(matches) == 0 {
			return nil
		}
		filesScanned++
		for _, line := range matches {
			out = append(out, line)
			if len(out) >= grepMaxResults {
				break
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return "", walkErr
	}

	if len(out) == 0 {
		return "No matches found.", nil
	}
	trunc := ""
	if len(out) >= grepMaxResults {
		trunc = fmt.Sprintf("\n... (truncated at %d matches)", grepMaxResults)
	}
	return strings.Join(out, "\n") + trunc, nil
}

// grepFile returns "path:line:content" lines for regex matches; second result
// reports whether the file looks binary (NUL byte in first 8 KiB).
func grepFile(path string, re *regexp.Regexp) ([]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	br := bufio.NewReader(f)
	// binary sniff: peek first 512 bytes for NUL
	head, _ := br.Peek(512)
	if strings.IndexByte(string(head), 0) >= 0 {
		return nil, true
	}

	var matches []string
	lineNo := 0
	for {
		line, rerr := br.ReadString('\n')
		if len(line) > 0 {
			lineNo++
			trimmed := strings.TrimRight(line, "\r\n")
			if re.MatchString(trimmed) {
				content := trimmed
				if len(content) > 300 {
					content = content[:300] + "…"
				}
				matches = append(matches, fmt.Sprintf("%s:%d:%s", path, lineNo, content))
				if len(matches) >= grepMaxResults {
					return matches, false
				}
			}
		}
		if rerr != nil {
			return matches, false
		}
	}
}
