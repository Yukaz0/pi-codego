package tools

import (
	"context"
	"fmt"
	"sync"

	"pi/pkg/types"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]types.Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]types.Tool),
	}
}

func (r *Registry) Register(tool types.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (types.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Definitions() []types.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]types.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	return tool.Execute(ctx, argsJSON)
}

func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewReadFileTool())
	r.Register(NewWriteFileTool())
	r.Register(NewEditFileTool())
	r.Register(NewBashTool())
	r.Register(NewGlobTool())
	r.Register(NewGrepTool())
	return r
}
