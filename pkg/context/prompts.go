package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
		// No frontmatter: fall back to first non-empty body line as the
		// description, mirroring Pi's loadTemplateFromFile.
		t.Description = firstLineDescription(content)
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
		t.Description = firstLineDescription(content)
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
	if t.Description == "" {
		t.Description = firstLineDescription(t.Content)
	}
	return t
}

// firstLineDescription derives a short description from the first non-empty
// line, truncating to 60 chars like Pi does.
func firstLineDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			if len(s) > 60 {
				return s[:60] + "..."
			}
			return s
		}
	}
	return ""
}

// parseCommandArgs splits an argument string respecting bash-style single and
// double quotes, so `/deploy "staging cluster" 42` yields two args. This
// mirrors Pi's parseCommandArgs.
func parseCommandArgs(argsString string) []string {
	var args []string
	var current strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(argsString); i++ {
		ch := argsString[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			} else {
				current.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			inQuote = ch
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// substituteArgsRe matches the Pi placeholder grammar:
//
//	${N:-default} | ${@:-default} | ${ARGUMENTS:-default}
//	${@:N} | ${@:N:L}
//	$1..$N | $@ | $ARGUMENTS
var substituteArgsRe = regexp.MustCompile(
	`\$\{(\d+|ARGUMENTS|@):-([^}]*)\}|\$\{@:(\d+)(?::(\d+))?\}|\$(ARGUMENTS|@|\d+)`)

// substituteArgs replaces placeholders in template content with argument
// values. Replacement happens on the template only — argument values are never
// recursively substituted. Mirrors Pi's substituteArgs.
func substituteArgs(content string, args []string) string {
	allArgs := strings.Join(args, " ")
	return substituteArgsRe.ReplaceAllStringFunc(content, func(match string) string {
		g := substituteArgsRe.FindStringSubmatch(match)
		// g: [full, defaultTarget, defaultValue, sliceStart, sliceLen, simple]
		if g[1] != "" { // ${X:-default}
			var val string
			if g[1] == "@" || g[1] == "ARGUMENTS" {
				val = allArgs
			} else if n, err := strconv.Atoi(g[1]); err == nil && n >= 1 && n <= len(args) {
				val = args[n-1]
			}
			if val != "" {
				return val
			}
			return g[2]
		}
		if g[3] != "" { // ${@:N} or ${@:N:L}
			start, _ := strconv.Atoi(g[3])
			start-- // 1-indexed -> 0-indexed
			if start < 0 {
				start = 0
			}
			if start > len(args) {
				return ""
			}
			if g[4] != "" {
				length, _ := strconv.Atoi(g[4])
				end := start + length
				if end > len(args) {
					end = len(args)
				}
				return strings.Join(args[start:end], " ")
			}
			return strings.Join(args[start:], " ")
		}
		if g[5] != "" { // $1 / $@ / $ARGUMENTS
			if g[5] == "@" || g[5] == "ARGUMENTS" {
				return allArgs
			}
			if n, err := strconv.Atoi(g[5]); err == nil && n >= 1 && n <= len(args) {
				return args[n-1]
			}
			return ""
		}
		return match
	})
}

// Expand substitutes Pi-style placeholders. Like Pi, arguments are only
// injected where a placeholder exists; a template with no placeholder is sent
// verbatim (extra args are dropped, not appended).
func (t PromptTemplate) Expand(args string) string {
	return strings.TrimSpace(substituteArgs(t.Content, parseCommandArgs(args)))
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
