package tui

import (
	"pi/pkg/mcp"
)

var globalMCP *mcp.Manager

func SetMCPManager(m *mcp.Manager) {
	globalMCP = m
}

func GetMCPManager() *mcp.Manager {
	return globalMCP
}
