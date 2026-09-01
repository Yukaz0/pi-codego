package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi/pkg/agent"
	piContext "pi/pkg/context"
)

func newLoaderTestModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// project skills: one nested SKILL.md, one loose .md
	skillDir := filepath.Join(home, ".pi", "agent", "skills", "pdf-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: pdf-tools\ndescription: Work with PDF files\n---\nUse pdftotext to extract text.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// prompt template
	promptDir := filepath.Join(home, ".pi", "agent", "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tplMD := "---\ndescription: Fix an issue\nargument-hint: [issue-id]\n---\nFix issue $1 now.\n"
	if err := os.WriteFile(filepath.Join(promptDir, "fix.md"), []byte(tplMD), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := agent.NewEngine(agent.EngineConfig{
		SkillLoader:  piContext.NewSkillLoader(home),
		PromptLoader: piContext.NewPromptLoader(home),
	})
	return NewModel(eng)
}

func TestSkillSlashCommandExpandsToSkillBlock(t *testing.T) {
	m := newLoaderTestModel(t)
	handled, cmd, expanded := handleSlash(m, "/skill:pdf-tools extract page 3")
	if !handled || cmd != nil {
		t.Fatalf("handled=%v cmd=%v", handled, cmd)
	}
	if !strings.HasPrefix(expanded, `<skill name="pdf-tools"`) {
		t.Errorf("expansion = %q", expanded)
	}
	if !strings.Contains(expanded, "Use pdftotext to extract text.") {
		t.Errorf("body missing: %q", expanded)
	}
	if !strings.Contains(expanded, "References are relative to") {
		t.Errorf("location line missing: %q", expanded)
	}
	if !strings.HasSuffix(expanded, "extract page 3") {
		t.Errorf("args not appended: %q", expanded)
	}
	// frontmatter must be stripped from the injected body
	if strings.Contains(expanded, "description: Work with PDF files") {
		t.Errorf("frontmatter leaked: %q", expanded)
	}
}

func TestUnknownSkillReportsNotFound(t *testing.T) {
	m := newLoaderTestModel(t)
	handled, _, expanded := handleSlash(m, "/skill:nope arg")
	if !handled || expanded != "" {
		t.Fatalf("handled=%v expanded=%q", handled, expanded)
	}
	if !strings.Contains(m.viewport.View(), "not found") {
		t.Errorf("expected not-found notice in viewport")
	}
}

func TestSlashSuggestionsIncludeSkillsAndTemplates(t *testing.T) {
	m := newLoaderTestModel(t)
	names := map[string]bool{}
	for _, s := range m.slashSuggestions("/") {
		names[s.Name] = true
	}
	if !names["skill:pdf-tools"] {
		t.Errorf("skill missing from popup: %v", names)
	}
	if !names["fix"] {
		t.Errorf("template missing from popup: %v", names)
	}
	// prefix filtering works for the skill: namespace too
	filtered := m.slashSuggestions("/skill:p")
	if len(filtered) != 1 || filtered[0].Name != "skill:pdf-tools" {
		t.Errorf("/skill:p = %+v", filtered)
	}
}

func TestTemplateSlashExpandsWithQuotedArgs(t *testing.T) {
	m := newLoaderTestModel(t)
	handled, _, expanded := handleSlash(m, `/fix "1234 core"`)
	if !handled {
		t.Fatal("not handled")
	}
	if expanded != `Fix issue 1234 core now.` {
		t.Errorf("expanded = %q", expanded)
	}
}

func TestHelpListsTemplatesAndSkills(t *testing.T) {
	m := newLoaderTestModel(t)
	handleHelp(m, "")
	view := m.viewport.View()
	if !strings.Contains(view, "Prompt templates") || !strings.Contains(view, "/fix") {
		t.Errorf("templates section missing from /help")
	}
	if !strings.Contains(view, "Agent skills") || !strings.Contains(view, "/skill:pdf-tools") {
		t.Errorf("skills section missing from /help")
	}
}
