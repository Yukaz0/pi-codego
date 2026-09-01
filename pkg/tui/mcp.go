package tui

import (
	"sync"

	"pi/pkg/mcp"

	tea "github.com/charmbracelet/bubbletea"
)

var globalMCP *mcp.Manager

func SetMCPManager(m *mcp.Manager) {
	globalMCP = m
}

func GetMCPManager() *mcp.Manager {
	return globalMCP
}

// StatusNotice is an async chat notice delivered to the running program
// (e.g. MCP background-load progress). Handled in Update like statusMsg.
type StatusNotice string

// mcpNotifier is wired to the live tea.Program once it exists; messages
// produced before that (startup races) are buffered and flushed on attach.
var (
	notifierMu   sync.Mutex
	notifier     func(tea.Msg)
	notifierPend []tea.Msg
)

// NotifyStatus sends a chat notice to the running TUI, buffering it if the
// program is not attached yet. Replaces stderr writes that would corrupt the
// alternate screen.
func NotifyStatus(text string) {
	notifierMu.Lock()
	defer notifierMu.Unlock()
	msg := StatusNotice(text)
	if notifier != nil {
		notifier(msg)
		return
	}
	notifierPend = append(notifierPend, msg)
}

// AttachNotifier connects the live program sender and flushes buffered notices.
func AttachNotifier(send func(tea.Msg)) {
	notifierMu.Lock()
	defer notifierMu.Unlock()
	notifier = send
	for _, m := range notifierPend {
		send(m)
	}
	notifierPend = nil
}
