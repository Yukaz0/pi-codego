package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	BaseDir     string `json:"baseDir"` // directory containing SKILL.md (references resolve here)
}

type SkillLoader struct {
	SkillDirs []string
}

// NewSkillLoader discovers skill dirs the way Pi does: project .pi/skills and
// global ~/.pi/agent/skills are the canonical locations; the older .agents,
// workspace ./skills, ~/.pi/skills and Gemini plugin dirs are kept as
// additional sources for compatibility.
func NewSkillLoader(workspaceDir string) *SkillLoader {
	dirs := []string{
		filepath.Join(workspaceDir, ".pi", "skills"),
		filepath.Join(workspaceDir, ".agents", "skills"),
		filepath.Join(workspaceDir, "skills"),
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".pi", "agent", "skills"))
		dirs = append(dirs, filepath.Join(home, ".pi", "skills"))
		dirs = append(dirs, filepath.Join(home, ".gemini", "config", "plugins"))
	}

	return &SkillLoader{
		SkillDirs: dirs,
	}
}

// LoadAvailableSkills walks each source dir. Discovery rules mirror Pi:
// a directory containing SKILL.md is a skill root (no recursion into it);
// otherwise direct .md children are loaded and subdirectories are recursed.
// Earlier dirs win on name collisions, so project skills override user skills.
func (s *SkillLoader) LoadAvailableSkills() ([]Skill, error) {
	var skills []Skill
	seen := map[string]bool{}
	seenPath := map[string]bool{}

	for _, dir := range s.SkillDirs {
		scanSkillDir(dir, true, seen, seenPath, &skills)
	}

	return skills, nil
}

// scanSkillDir collects skills under dir. includeRootFiles mirrors Pi: loose
// .md files count as skills only directly inside a configured source dir;
// subdirectories are only entered to find SKILL.md roots.
func scanSkillDir(dir string, includeRootFiles bool, seen, seenPath map[string]bool, out *[]Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Rule 1: dir containing SKILL.md is a skill root — load it and stop.
	for _, e := range entries {
		if e.Name() != "SKILL.md" {
			continue
		}
		path := filepath.Join(dir, "SKILL.md")
		if !isReadableFile(path) {
			continue
		}
		addSkillFromFile(path, dir, seen, seenPath, out)
		return
	}

	// Rule 2: loose .md children (only inside an explicit skills source dir),
	// then recurse into subdirectories looking for SKILL.md roots.
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			scanSkillDir(path, false, seen, seenPath, out)
			continue
		}
		if includeRootFiles && strings.HasSuffix(e.Name(), ".md") && isReadableFile(path) {
			addSkillFromFile(path, dir, seen, seenPath, out)
		}
	}
}

func isReadableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func addSkillFromFile(path, baseDir string, seen, seenPath map[string]bool, out *[]Skill) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		if seenPath[real] {
			return // same file reached twice via symlink
		}
		seenPath[real] = true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	name, desc := parseSkillFrontmatter(content)
	if name == "" {
		if base := filepath.Base(path); base == "SKILL.md" {
			name = filepath.Base(baseDir)
		} else {
			name = strings.TrimSuffix(base, filepath.Ext(base))
			name = filepath.Base(name)
		}
	}
	if seen[name] {
		return // earlier (higher-priority) dir wins on collision
	}
	seen[name] = true
	*out = append(*out, Skill{
		Name:        name,
		Description: desc,
		Path:        path,
		Content:     content,
		BaseDir:     baseDir,
	})
}

func parseSkillFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return name, description
}

// StripSkillFrontmatter removes a leading --- block, returning the body.
func StripSkillFrontmatter(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return content
}

// FormatSkillBlock wraps a skill's body for injection as a user message when
// the user invokes /skill:<name>, mirroring Pi's _expandSkillCommand.
func FormatSkillBlock(sk Skill) string {
	body := StripSkillFrontmatter(sk.Content)
	return fmt.Sprintf("<skill name=%q location=%q>\nReferences are relative to %s.\n\n%s\n</skill>",
		sk.Name, sk.Path, sk.BaseDir, body)
}

func FormatSkillsPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\nAvailable Agent Skills:\n")
	for _, sk := range skills {
		sb.WriteString(fmt.Sprintf("- %s: %s (path: %s)\n", sk.Name, sk.Description, sk.Path))
	}
	return sb.String()
}
