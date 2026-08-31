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
}

type SkillLoader struct {
	SkillDirs []string
}

func NewSkillLoader(workspaceDir string) *SkillLoader {
	dirs := []string{
		filepath.Join(workspaceDir, ".agents", "skills"),
		filepath.Join(workspaceDir, "skills"),
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".pi", "skills"))
		dirs = append(dirs, filepath.Join(home, ".gemini", "config", "plugins"))
	}

	return &SkillLoader{
		SkillDirs: dirs,
	}
}

func (s *SkillLoader) LoadAvailableSkills() ([]Skill, error) {
	var skills []Skill

	for _, dir := range s.SkillDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			skillContent := string(data)
			name, desc := parseSkillFrontmatter(skillContent)
			if name == "" {
				name = entry.Name()
			}

			skills = append(skills, Skill{
				Name:        name,
				Description: desc,
				Path:        skillFile,
				Content:     skillContent,
			})
		}
	}

	return skills, nil
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
