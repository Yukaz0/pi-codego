package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pi/pkg/session"
)

// sessionUsage accumulates token usage across turns for the footer.
type sessionUsage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
	ContextUsed  int // last prompt size = context currently in use
}

// newSessionID generates a Pi-like sortable session id.
func newSessionID() string {
	return fmt.Sprintf("%s-%d", time.Now().Format("2006-01-02T15-04-05"), os.Getpid())
}

// ensureStorage lazily creates the session storage.
func (m *Model) ensureStorage() *session.Storage {
	if m.storage == nil {
		m.storage = session.NewStorage("")
	}
	return m.storage
}

// autoSaveSession persists the current tree after each completed turn.
func (m *Model) autoSaveSession() {
	if m.engine == nil || m.engine.Config.SessionTree == nil {
		return
	}
	if m.sessionID == "" {
		m.sessionID = newSessionID()
	}
	if err := m.ensureStorage().SaveSession(m.sessionID, m.engine.Config.SessionTree); err != nil {
		m.appendSystem("session save failed: " + err.Error())
	}
}

// switchToTree replaces the engine's session tree and refreshes the view.
func (m *Model) switchToTree(tree *session.Tree, sessionID string, label string) {
	m.engine.Config.SessionTree = tree
	if m.engine.Config.Compactor != nil {
		// compactor is stateless w.r.t. tree; nothing to rewire
		_ = struct{}{}
	}
	m.sessionID = sessionID
	m.sessionName = tree.GetName()
	m.history = tree.GetLinearHistory("")
	m.refreshViewport()
	m.appendSystem(fmt.Sprintf("— %s —", label))
}

// handleNew starts a fresh session (old one stays saved on disk).
func handleNew(m *Model, _ string) tea.Cmd {
	m.engine.Config.SessionTree = session.NewTree()
	m.sessionID = ""
	m.sessionName = ""
	m.history = nil
	m.streamBuffer.Reset()
	m.viewport.SetContent(statusStyle.Render("— New session started —") + "\n" + helpStyle.Render("Type /help for commands, or just ask a question."))
	m.viewport.GotoBottom()
	return nil
}

// handleResume lists saved sessions and loads the selected one.
func handleResume(m *Model, args string) tea.Cmd {
	store := m.ensureStorage()
	if a := strings.TrimSpace(args); a != "" {
		return loadSessionByID(m, store, a)
	}
	infos := store.ListSessions()
	if len(infos) == 0 {
		m.viewport.SetContent(statusStyle.Render("No previous sessions found in " + store.SessionDir))
		m.viewport.GotoBottom()
		return nil
	}
	items := make([]string, 0, len(infos))
	byID := map[string]session.SessionInfo{}
	for _, si := range infos {
		name := si.Name
		if name == "" {
			name = "(unnamed)"
		}
		first := si.FirstMsg
		if first == "" {
			first = "(no user message)"
		}
		label := fmt.Sprintf("%s — %s — %d msgs — %s — %s",
			si.ID, name, si.MessageCnt, si.Modified.Format("01-02 15:04"), first)
		items = append(items, label)
		byID[si.ID] = si
	}
	m.openPicker("Resume session — Enter to load", items, func(sel string) tea.Cmd {
		id := strings.Fields(sel)[0]
		return loadSessionByID(m, store, id)
	}, true)
	return nil
}

func loadSessionByID(m *Model, store *session.Storage, id string) tea.Cmd {
	tree, err := store.LoadSession(id)
	if err != nil {
		m.appendSystem(errorStyle.Render(fmt.Sprintf("Failed to load session %s: %v", id, err)))
		return nil
	}
	m.switchToTree(tree, id, "Resumed session "+id)
	return nil
}

// LoadSessionByID resumes a saved session (used by --session flag before the
// program starts, so no tea.Cmd is involved).
func (m *Model) LoadSessionByID(id string) error {
	tree, err := m.ensureStorage().LoadSession(id)
	if err != nil {
		return err
	}
	m.switchToTree(tree, id, "Resumed session "+id)
	return nil
}

// ContinueMostRecent resumes the newest saved session (--continue).
func (m *Model) ContinueMostRecent() error {
	infos := m.ensureStorage().ListSessions()
	if len(infos) == 0 {
		return fmt.Errorf("no previous sessions to continue")
	}
	return m.LoadSessionByID(infos[0].ID)
}

// handleFork branches the conversation from an earlier message: the next
// user message will be appended as a sibling at that point (Pi /fork).
func handleFork(m *Model, args string) tea.Cmd {
	tree := m.engine.Config.SessionTree
	if tree == nil || len(tree.Nodes) == 0 {
		m.appendSystem("Nothing to fork — empty session")
		return nil
	}
	hist := tree.GetLinearHistory("")
	n := len(hist)
	if args != "" {
		if _, err := fmt.Sscanf(args, "%d", &n); err != nil || n < 1 {
			m.appendSystem("Usage: /fork <message-number> — see /tree for numbering")
			return nil
		}
	}
	if n > len(hist) {
		n = len(hist)
	}
	// fork from the message BEFORE the chosen one, so the chosen message
	// itself becomes the first new entry on the branch (Pi semantics:
	// editing message N forks at N-1).
	idx := n - 2
	if idx < 0 {
		m.engine.Config.SessionTree = session.NewTree()
		m.sessionID = ""
		m.history = nil
		m.appendSystem("Forked from the beginning — next message starts a new branch")
		return nil
	}
	target := hist[idx]
	if err := tree.NewBranchFrom(target.ID); err != nil {
		m.appendSystem("Fork failed: " + err.Error())
		return nil
	}
	m.history = tree.GetLinearHistory("")
	m.refreshViewport()
	m.appendSystem(fmt.Sprintf("Forked at message %d (%s) — your next message branches there", n, target.ID[:min(6, len(target.ID))]))
	return nil
}

// handleClone saves a copy of the current linear path as a new session file
// and continues in it (Pi /clone).
func handleClone(m *Model, _ string) tea.Cmd {
	tree := m.engine.Config.SessionTree
	if tree == nil || len(tree.Nodes) == 0 {
		m.appendSystem("Nothing to clone — empty session")
		return nil
	}
	clone := tree.CloneLinear()
	clone.SetName(tree.GetName())
	newID := newSessionID() + "-clone"
	if err := m.ensureStorage().SaveSession(newID, clone); err != nil {
		m.appendSystem("Clone failed: " + err.Error())
		return nil
	}
	m.switchToTree(clone, newID, "Cloned session → "+newID)
	return nil
}

// handleName sets the session display name (persisted in the session file).
func handleName(m *Model, args string) tea.Cmd {
	name := strings.TrimSpace(args)
	if name == "" {
		m.viewport.SetContent(errorStyle.Render("Usage: /name <session-name>"))
		m.viewport.GotoBottom()
		return nil
	}
	m.sessionName = name
	if m.engine.Config.SessionTree != nil {
		m.engine.Config.SessionTree.SetName(name)
		m.autoSaveSession()
	}
	m.appendSystem("Session renamed to: " + name)
	return nil
}

// handleSessionInfo shows session id/name/path/model/usage.
func handleSessionInfo(m *Model, _ string) tea.Cmd {
	tree := m.engine.Config.SessionTree
	if tree == nil {
		m.viewport.SetContent(statusStyle.Render("No active session"))
		m.viewport.GotoBottom()
		return nil
	}
	hist := tree.GetLinearHistory("")
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Session ") + "\n")
	id := m.sessionID
	if id == "" {
		id = "(unsaved — saves after first turn)"
	}
	name := tree.GetName()
	if name == "" {
		name = "(unnamed)"
	}
	sb.WriteString(fmt.Sprintf("id:      %s\nname:    %s\nfile:    %s\nmessages: %d (tree nodes: %d)\nmodel:   %s\n",
		id, name,
		filepath.Join(m.ensureStorage().SessionDir, m.sessionID+".jsonl"),
		len(hist), len(tree.Nodes), m.engine.Config.Model))
	sb.WriteString(fmt.Sprintf("est. tokens: ~%d\n", session.EstimateTokens(hist)))
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
	return nil
}
