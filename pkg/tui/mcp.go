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
// alternate screen. The send happens outside the mutex because tea.Program.Send
// blocks until the event loop drains the message.
func NotifyStatus(text string) {
	msg := StatusNotice(text)
	notifierMu.Lock()
	n := notifier
	if n == nil {
		notifierPend = append(notifierPend, msg)
		notifierMu.Unlock()
		return
	}
	notifierMu.Unlock()
	n(msg)
}

// AttachNotifier connects the live program sender and flushes buffered notices.
// The flush runs in its own goroutine: Send blocks until the program's event
// loop is running, and AttachNotifier is called *before* p.Run(), so a
// synchronous flush here would deadlock the whole TUI (v0.4.1 regression).
func AttachNotifier(send func(tea.Msg)) {
	notifierMu.Lock()
	notifier = send
	pending := notifierPend
	notifierPend = nil
	notifierMu.Unlock()
	if len(pending) == 0 {
		return
	}
	go func() {
		for _, m := range pending {
			send(m)
		}
	}()
}
