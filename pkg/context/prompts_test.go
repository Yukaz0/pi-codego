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

func TestPromptTemplateNoPlaceholderDropsArgs(t *testing.T) {
	// Pi behavior: a template without placeholders is sent verbatim;
	// extra args are dropped, not appended.
	tpl := parsePromptTemplate("sum", "Summarize this file.", "/x/sum.md")
	got := tpl.Expand("main.go")
	if got != "Summarize this file." {
		t.Errorf("expected verbatim body, got %q", got)
	}
}

func TestPromptTemplateQuotedArgs(t *testing.T) {
	tpl := parsePromptTemplate("deploy", "env=$1 rest=$2", "/x/deploy.md")
	got := tpl.Expand(`"staging cluster" 42`)
	if got != "env=staging cluster rest=42" {
		t.Errorf("quoted args = %q", got)
	}
}

func TestPromptTemplateDefaultsAndSlices(t *testing.T) {
	tpl := parsePromptTemplate("t",
		"a=${1:-fallback} b=${2:-two} all=${@:-none} from=${@:2} pair=${@:1:2}", "/x/t.md")
	if got := tpl.Expand(""); got != "a=fallback b=two all=none from= pair=" {
		t.Errorf("empty args = %q", got)
	}
	if got := tpl.Expand("x y z"); got != "a=x b=y all=x y z from=y z pair=x y" {
		t.Errorf("full args = %q", got)
	}
}

func TestPromptTemplateNoRecursiveSubstitution(t *testing.T) {
	// Argument values containing $1/$@ must NOT be substituted again.
	tpl := parsePromptTemplate("t", "v=$1 w=$@", "/x/t.md")
	if got := tpl.Expand("$1 $@"); got != "v=$1 w=$1 $@" {
		t.Errorf("recursive = %q", got)
	}
}

func TestPromptTemplateDescriptionFallback(t *testing.T) {
	tpl := parsePromptTemplate("t", "First line is the description.\nmore", "/x/t.md")
	if tpl.Description != "First line is the description." {
		t.Errorf("desc = %q", tpl.Description)
	}
	long := strings.Repeat("x", 80)
	if got := parsePromptTemplate("t", long, "/x/t.md").Description; got != long[:60]+"..." {
		t.Errorf("truncation = %q", got)
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
