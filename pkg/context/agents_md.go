package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InstructionLoader struct {
	WorkspaceDir string
}

func NewInstructionLoader(workspaceDir string) *InstructionLoader {
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}
	return &InstructionLoader{
		WorkspaceDir: workspaceDir,
	}
}

// LoadInstructions searches for AGENTS.md hierarchically (cwd -> parents) plus SYSTEM.md override
func (l *InstructionLoader) LoadInstructions() (systemPromptOverride string, projectRules string, err error) {
	// Check SYSTEM.md override (workspace + parent walk)
	for _, candidate := range []string{filepath.Join(l.WorkspaceDir, "SYSTEM.md"), filepath.Join(l.WorkspaceDir, ".SYSTEM.md")} {
		if data, err := os.ReadFile(candidate); err == nil {
			systemPromptOverride = strings.TrimSpace(string(data))
			break
		}
	}

	// Hierarchical AGENTS.md search: walk from workspace up to root
	var ruleFiles []string
	candidateNames := []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".agents.md"}
	// Walk up directories
	dir := l.WorkspaceDir
	var visited []string
	for {
		for _, name := range candidateNames {
			p := filepath.Join(dir, name)
			if data, err := os.ReadFile(p); err == nil {
				ruleFiles = append(ruleFiles, fmt.Sprintf("=== Rules from %s ===\n%s", p, strings.TrimSpace(string(data))))
			}
		}
		visited = append(visited, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		// Limit walk depth to avoid excessive I/O
		if len(visited) > 10 {
			break
		}
	}

	// Check global ~/.pi/agent/AGENTS.md if exists
	home, err := os.UserHomeDir()
	if err == nil {
		globalAgents := filepath.Join(home, ".pi", "agent", "AGENTS.md")
		if data, err := os.ReadFile(globalAgents); err == nil {
			ruleFiles = append(ruleFiles, fmt.Sprintf("=== Global Rules from ~/.pi/agent/AGENTS.md ===\n%s", strings.TrimSpace(string(data))))
		}
	}

	if len(ruleFiles) > 0 {
		projectRules = strings.Join(ruleFiles, "\n\n")
	}

	return systemPromptOverride, projectRules, nil
}
