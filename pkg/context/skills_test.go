package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillLoaderDiscoversNestedAndLoose(t *testing.T) {
	root := t.TempDir()
	// nested SKILL.md root
	writeSkill(t, filepath.Join(root, "pack", "sub", "SKILL.md"),
		"---\nname: deep\ndescription: deep skill\n---\nBODY-DEEP\n")
	// loose .md directly in the source dir
	writeSkill(t, filepath.Join(root, "flat.md"), "---\nname: flat\n---\nBODY-FLAT\n")
	// dir without any md — ignored
	writeSkill(t, filepath.Join(root, "notes", "readme.txt"), "ignore me")

	loader := &SkillLoader{SkillDirs: []string{root}}
	skills, err := loader.LoadAvailableSkills()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	if len(byName) != 2 {
		t.Fatalf("skills = %+v", skills)
	}
	deep := byName["deep"]
	if deep.Description != "deep skill" || deep.Content == "" {
		t.Errorf("deep = %+v", deep)
	}
	if filepath.Base(deep.BaseDir) != "sub" {
		t.Errorf("deep.BaseDir = %q", deep.BaseDir)
	}
	// loose .md without name frontmatter falls back to file stem
	if _, ok := byName["flat"]; !ok {
		t.Errorf("flat skill missing: %+v", skills)
	}
}

func TestSkillLoaderProjectOverridesUser(t *testing.T) {
	proj := t.TempDir()
	user := t.TempDir()
	writeSkill(t, filepath.Join(proj, "pdf", "SKILL.md"), "---\nname: pdf\ndescription: project\n---\nP\n")
	writeSkill(t, filepath.Join(user, "pdf", "SKILL.md"), "---\nname: pdf\ndescription: user\n---\nU\n")

	loader := &SkillLoader{SkillDirs: []string{proj, user}}
	skills, _ := loader.LoadAvailableSkills()
	if len(skills) != 1 || skills[0].Description != "project" {
		t.Fatalf("expected project override, got %+v", skills)
	}
}

func TestFormatSkillBlockStripsFrontmatter(t *testing.T) {
	sk := Skill{
		Name:    "pdf-tools",
		Path:    "/x/pdf-tools/SKILL.md",
		BaseDir: "/x/pdf-tools",
		Content: "---\nname: pdf-tools\n---\nDo the thing.\n",
	}
	block := FormatSkillBlock(sk)
	if want := `<skill name="pdf-tools" location="/x/pdf-tools/SKILL.md">`; len(block) < len(want) || block[:len(want)] != want {
		t.Errorf("header = %q", block[:60])
	}
	if !strings.Contains(block, "References are relative to /x/pdf-tools.") || !strings.Contains(block, "Do the thing.") {
		t.Errorf("block = %q", block)
	}
	if strings.Contains(block, "name: pdf-tools\n---") {
		t.Errorf("frontmatter leaked: %q", block)
	}
}
