package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePromptTemplate(t *testing.T) {
	src := "---\ndescription: Fix an issue\nargument-hint: [issue-id]\n---\nPlease fix issue $1 carefully.\nContext: $@\n"
	tpl := parsePromptTemplate("fix", src, "/x/fix.md")
	if tpl.Description != "Fix an issue" {
		t.Errorf("description = %q", tpl.Description)
	}
	if tpl.ArgumentHint != "[issue-id]" {
		t.Errorf("hint = %q", tpl.ArgumentHint)
	}
	got := tpl.Expand("42 extra")
	if !strings.Contains(got, "fix issue 42 carefully") {
		t.Errorf("expand = %q", got)
	}
	if !strings.Contains(got, "Context: 42 extra") {
		t.Errorf("expand $@ = %q", got)
	}
}

func TestPromptTemplateNoPlaceholderAppends(t *testing.T) {
	tpl := parsePromptTemplate("sum", "Summarize this file.", "/x/sum.md")
	got := tpl.Expand("main.go")
	if !strings.HasSuffix(got, "main.go") {
		t.Errorf("expected args appended, got %q", got)
	}
}

func TestPromptLoaderProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, ".pi", "prompts")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "deploy.md"), []byte("project deploy $1"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := &PromptLoader{Dirs: []string{proj}}
	tpls := loader.LoadAll()
	if len(tpls) != 1 || tpls[0].Name != "deploy" {
		t.Fatalf("templates = %+v", tpls)
	}
	if got, ok := loader.Get("DEPLOY"); !ok || got.Name != "deploy" {
		t.Errorf("case-insensitive Get failed: %v", ok)
	}
}
