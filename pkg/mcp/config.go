package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ServerConfig describes one MCP server (stdio)
type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Disabled omits server
	Disabled bool `json:"disabled,omitempty"`
}

type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

func ConfigPaths() []string {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	return []string{
		filepath.Join(wd, ".pi", "mcp.json"),
		filepath.Join(wd, ".mcp.json"),
		filepath.Join(home, ".pi", "agent", "mcp.json"),
		filepath.Join(home, ".config", "pi", "mcp.json"),
		filepath.Join(home, ".pi", "mcp.json"),
	}
}

func LoadConfig() (*Config, string, error) {
	for _, p := range ConfigPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if cfg.MCPServers == nil {
			cfg.MCPServers = make(map[string]ServerConfig)
		}
		return &cfg, p, nil
	}
	return &Config{MCPServers: make(map[string]ServerConfig)}, "", nil
}

func SaveExampleConfig() (string, error) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "", os.ErrNotExist
	}
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp.json")
	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}
	example := Config{
		MCPServers: map[string]ServerConfig{
			"filesystem": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/home/neu/Documents"},
			},
			"codebase-memory": {
				Command: "npx",
				Args:    []string{"-y", "codebase-memory-mcp"},
			},
		},
	}
	data, _ := json.MarshalIndent(example, "", "  ")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}
