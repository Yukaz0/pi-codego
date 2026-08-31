package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PromptTemplate is a reusable prompt loaded from a .md file, mirroring the
// Pi prompt-template feature (.pi/prompts/*.md and ~/.pi/agent/prompts/*.md).
// It is triggered as a slash command: /name [args...].
type PromptTemplate struct {
	Name         string
	Description  string
	ArgumentHint string
	Content      string // body without frontmatter
	Path         string
}

type PromptLoader struct {
	Dirs []string
}

// NewPromptLoader discovers prompt template dirs: project .pi/prompts (walked
// up from workspace like AGENTS.md) plus global ~/.pi/agent/prompts.
func NewPromptLoader(workspaceDir string) *PromptLoader {
	var dirs []string
	dir := workspaceDir
	for i := 0; i < 10 && dir != ""; i++ {
		candidate := filepath.Join(dir, ".pi", "prompts")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			dirs = append(dirs, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".pi", "agent", "prompts"))
	}
	return &PromptLoader{Dirs: dirs}
}

// LoadAll returns templates, project-level overriding global by name.
func (p *PromptLoader) LoadAll() []PromptTemplate {
	byName := map[string]PromptTemplate{}
	var order []string
	// load global first so project overrides it
	for i := len(p.Dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(p.Dirs[i])
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(p.Dirs[i], e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			t := parsePromptTemplate(e.Name()[:len(e.Name())-3], string(data), path)
			if _, exists := byName[t.Name]; !exists {
				order = append(order, t.Name)
			}
			byName[t.Name] = t
		}
	}
	out := make([]PromptTemplate, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}

// Get returns a template by name (case-insensitive).
func (p *PromptLoader) Get(name string) (PromptTemplate, bool) {
	for _, t := range p.LoadAll() {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return PromptTemplate{}, false
}

func parsePromptTemplate(name, content, path string) PromptTemplate {
	t := PromptTemplate{Name: name, Content: content, Path: path}
	if !strings.HasPrefix(content, "---") {
		return t
	}
	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return t
	}
	for _, line := range lines[1:end] {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			t.Description = strings.Trim(strings.TrimSpace(v), `"'`)
		} else if v, ok := strings.CutPrefix(line, "argument-hint:"); ok {
			t.ArgumentHint = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	t.Content = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return t
}

var positionalRe = regexp.MustCompile(`\$[1-9][0-9]*`)

// Expand substitutes Pi-style placeholders:
//
//	$1..$N   positional argument N
//	$@       all arguments joined by space
//	$ARGUMENTS  alias of $@
//
// If no placeholder exists and args are non-empty, args are appended.
func (t PromptTemplate) Expand(args string) string {
	fields := strings.Fields(args)
	body := t.Content
	hasPlaceholder := positionalRe.MatchString(body) ||
		strings.Contains(body, "$@") || strings.Contains(body, "$ARGUMENTS")

	out := body
	for i, arg := range fields {
		out = strings.ReplaceAll(out, fmt.Sprintf("$%d", i+1), arg)
	}
	// unset positional refs become empty
	out = positionalRe.ReplaceAllString(out, "")
	out = strings.ReplaceAll(out, "$ARGUMENTS", strings.Join(fields, " "))
	out = strings.ReplaceAll(out, "$@", strings.Join(fields, " "))

	if !hasPlaceholder && len(fields) > 0 {
		out = out + "\n\n" + args
	}
	return strings.TrimSpace(out)
}

// FormatTemplatesPrompt lists templates in the system prompt so the model
// knows user shortcuts exist (Pi injects them into the startup header; here
// we keep it minimal and only for TUI display).
func FormatTemplatesPrompt(templates []PromptTemplate) string {
	if len(templates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nAvailable prompt templates (user invokes via /name):\n")
	for _, t := range templates {
		sb.WriteString(fmt.Sprintf("- /%s: %s\n", t.Name, t.Description))
	}
	return sb.String()
}
