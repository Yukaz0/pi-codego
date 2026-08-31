package types

import "context"

// ToolParameterSchema represents JSON Schema for tool parameters
type ToolParameterSchema struct {
	Type        string                 `json:"type"`
	Properties  map[string]PropertyDef `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Description string                 `json:"description,omitempty"`
}

type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type ToolDefinition struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Parameters  ToolParameterSchema `json:"parameters"`
}

// Tool is the interface all agent tools must implement
type Tool interface {
	Name() string
	Description() string
	Definition() ToolDefinition
	Execute(ctx context.Context, argsJSON string) (string, error)
}
