package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pi/pkg/agent"
	"pi/pkg/provider/modelcatalog"
	"pi/pkg/session"
	"pi/pkg/types"
)

// TUI messages
type contentDeltaMsg string
type toolStartMsg struct{ Tool, Args string }
type toolEndMsg struct {
	Tool, Result string
	IsError      bool
}
type turnDoneMsg struct{ Err error }
type statusMsg string
type gitTickMsg time.Time

type Model struct {
	engine *agent.Engine
	keys   KeyMap

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	width  int
	height int

	history      []types.Message
	streamBuffer strings.Builder
	streaming    bool
	showHelp     bool
	errMsg       string
	ctx          context.Context
	cancel       context.CancelFunc

	// TUI-specific display state
	thinkingMode string // "off", "minimal", "low", "medium", "high", "xhigh", "max"
	gitBranch    string
	tokens       int
	usage        *sessionUsage           // shared accumulator so listeners on Model copies report back
	slashCursor  int                     // highlighted row in the inline slash suggestion popup
	pendingImgs  []types.ImageAttachment // images pasted via Ctrl+V, attached on next submit
	keyAlias     map[string]string       // custom keybindings: override key -> canonical key
	bgTasks      map[string]string       // id -> label
	bgCounter    int
	sessionName  string

	// session persistence
	storage   *session.Storage
	sessionID string

	// generic picker — arrow + enter navigation for all pick operations
	picker pickerState

	// chat rendering cache: rendered string per history index, invalidated on
	// width change or non-append history mutation (reload/fork/compact/clear).
	chatCache      []string
	chatCacheWidth int
	chatStale      bool
	notices        []chatNotice // display-only lines anchored to history size
}

// chatNotice is a display-only chat line (system notice, bash result) that is
// not part of the model history. anchor = len(history) when it was inserted,
// so a full re-render keeps notices interleaved in place.
type chatNotice struct {
	anchor int
	text   string
}

type pickerState struct {
	active    bool
	title     string
	items     []string // filtered list (what user sees)
	baseItems []string // unfiltered source (for re-filter on query change)
	cursor    int
	onConfirm func(string) tea.Cmd
	// onDefault optionally handles Ctrl+D in the model picker: select the
	// highlighted item AND save it as the default model (settings.json).
	// Nil on every other picker, so the key is inert elsewhere.
	onDefault  func(string) tea.Cmd
	searchable bool
	query      textinput.Model
	queryEmpty bool // true until user types anything; controls Esc behavior
	// prompt mode (Pi-style login dialog): after selecting an item the
	// picker stays open and asks for a typed value (API key, URL, ...).
	prompt      bool
	promptLabel string
	promptValue string
	maskInput   bool
	onPrompt    func(string) (nextLabel string, finished bool)
	pendingCmd  tea.Cmd           // returned to the runtime when the prompt finishes
	scope       string            // provider scope when the picker lists models
	annotations map[string]string // optional right-hand note per item
}

func NewModel(engine *agent.Engine) *Model {
	ta := textarea.New()
	ta.Placeholder = "Ask Pi…  Enter to send  ·  / for commands  ·  ! for bash"
	ta.Focus()
	ta.CharLimit = 8000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	// Pi-style: no border, no background — plain editor with prompt cursor.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#8abeb7")).Bold(true)
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	ta.BlurredStyle.Text = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#8abeb7")).Bold(true)

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	ctx, cancel := context.WithCancel(context.Background())

	m := &Model{
		engine:       engine,
		keys:         DefaultKeyMap(),
		viewport:     vp,
		textarea:     ta,
		spinner:      sp,
		history:      []types.Message{},
		ctx:          ctx,
		cancel:       cancel,
		thinkingMode: "medium",
		gitBranch:    detectGitBranch(),
		bgTasks:      map[string]string{},
		usage:        &sessionUsage{},
		keyAlias:     loadKeyOverrides(),
	}
	// welcome line as an anchored notice so it survives re-renders until the
	// first real entry pushes it out of the way naturally.
	m.notices = []chatNotice{{anchor: 0, text: renderAssistantText("Welcome to Pi — coding agent. Type a message and press Enter, or `/help` for commands.")}}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick, gitTickCmd())
}

func gitTickCmd() tea.Cmd {
	return tea.Tick(5*60_000_000_000, func(t time.Time) tea.Msg { return gitTickMsg(t) })
}

func detectGitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.textarea.SetWidth(msg.Width - 4)
		m.refreshViewport() // re-wrap bubbles at the new width
		return m, nil

	case gitTickMsg:
		m.gitBranch = detectGitBranch()
		return m, gitTickCmd()

	case tea.KeyMsg:
		// picker takes priority
		if m.picker.active {
			// Prompt mode: the picker asks for a typed value (API key, URL).
			// All printable input goes to the prompt, not the chat textarea.
			if m.picker.prompt {
				switch msg.String() {
				case "esc", "ctrl+c", "ctrl+q":
					m.picker.active = false
					m.appendSystem("Login cancelled")
					return m, nil
				case "enter":
					val := m.picker.promptValue
					m.picker.promptValue = ""
					next, finished := m.picker.onPrompt(val)
					var cmd tea.Cmd
					if finished {
						cmd = m.picker.pendingCmd
						m.picker.pendingCmd = nil
						m.picker.active = false
					} else {
						m.picker.promptLabel = next
					}
					return m, cmd
				case "backspace":
					if len(m.picker.promptValue) > 0 {
						r := []rune(m.picker.promptValue)
						m.picker.promptValue = string(r[:len(r)-1])
					}
					return m, nil
				}
				if len(msg.Runes) > 0 {
					m.picker.promptValue += string(msg.Runes)
					return m, nil
				}
				return m, nil
			}
			// Route printable / backspace keys to the search input when searchable.
			// Esc clears the query first if non-empty, otherwise closes the picker.
			if m.picker.searchable {
				switch msg.String() {
				case "esc":
					if !m.picker.queryEmpty {
						m.picker.query.SetValue("")
						m.picker.queryEmpty = true
						m.recomputePicker()
						return m, nil
					}
					m.picker.active = false
					m.appendSystem(m.picker.title + " cancelled")
					return m, nil
				case "enter":
					cmd := m.confirmPicker()
					return m, cmd
				case "ctrl+d":
					if m.picker.onDefault != nil {
						cmd := m.confirmDefaultPicker()
						return m, cmd
					}
					// no hook: fall through to the query input
				case "up", "ctrl+p":
					if m.picker.cursor > 0 {
						m.picker.cursor--
					}
					return m, nil
				case "down", "ctrl+n":
					if m.picker.cursor < len(m.picker.items)-1 {
						m.picker.cursor++
					}
					return m, nil
				}
				// forward to textinput for printable + backspace + delete handling
				prev := m.picker.query.Value()
				var tiCmd tea.Cmd
				m.picker.query, tiCmd = m.picker.query.Update(msg)
				if m.picker.query.Value() != prev {
					m.picker.queryEmpty = m.picker.query.Value() == ""
					m.recomputePicker()
				}
				return m, tiCmd
			}
			// non-searchable picker — original behavior
			switch msg.String() {
			case "up", "k", "ctrl+p":
				if m.picker.cursor > 0 {
					m.picker.cursor--
				}
				return m, nil
			case "down", "j", "ctrl+n":
				if m.picker.cursor < len(m.picker.items)-1 {
					m.picker.cursor++
				}
				return m, nil
			case "enter":
				cmd := m.confirmPicker()
				return m, cmd
			case "ctrl+d":
				cmd := m.confirmDefaultPicker()
				return m, cmd
			case "esc", "ctrl+c", "ctrl+q":
				m.picker.active = false
				m.appendSystem(m.picker.title + " cancelled")
				return m, nil
			}
			return m, nil
		}
		// inline slash suggestions: arrows navigate, Tab/Shift+Tab complete
		if len(m.slashSuggestions(m.textarea.Value())) > 0 {
			switch msg.String() {
			case "down", "ctrl+n":
				m.slashCursor++
				return m, nil
			case "up", "ctrl+p":
				m.slashCursor--
				return m, nil
			case "tab":
				m.completeSlash()
				return m, nil
			case "shift+tab":
				m.slashCursor--
				m.completeSlash()
				return m, nil
			}
		}
		keyStr := msg.String()
		if canon, ok := m.keyAlias[keyStr]; ok {
			keyStr = canon
		}
		switch keyStr {
		case "ctrl+v":
			// paste image from clipboard (Wayland/xclip/image file path)
			return m, readClipboardImageMsg
		case "ctrl+c", "ctrl+q":
			if m.streaming {
				if m.cancel != nil {
					m.cancel()
					m.ctx, m.cancel = context.WithCancel(context.Background())
				}
				m.streaming = false
				m.appendSystem("[interrupted]")
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+l":
			m.history = nil
			m.streamBuffer.Reset()
			m.notices = nil
			m.chatCache = nil
			m.invalidateChatCache()
			m.refreshViewport()
			return m, nil
		case "?":
			// "?" toggles the help line only when the input is empty and
			// idle; otherwise it must reach the textarea so users can type
			// questions ending in "?".
			if strings.TrimSpace(m.textarea.Value()) == "" && !m.streaming {
				m.showHelp = !m.showHelp
				return m, nil
			}
		case "ctrl+p":
			cur := strings.TrimSpace(m.textarea.Value())
			filter := ""
			if strings.HasPrefix(cur, "/") {
				filter = strings.TrimPrefix(cur, "/")
			}
			m.openCommandPicker(filter)
			return m, nil
		case "ctrl+o":
			cur := strings.TrimSpace(m.textarea.Value())
			filter := ""
			if strings.HasPrefix(cur, "/model") {
				filter = strings.TrimSpace(strings.TrimPrefix(cur, "/model"))
			}
			m.openModelPicker(filter)
			return m, nil
		case "ctrl+t":
			// quick cycle thinking mode (off → minimal → low → medium → high → xhigh → max)
			m.cycleThinking()
			return m, nil
		case "y":
			if last := m.lastAssistantText(); last != "" {
				_ = copyToClipboard(last)
				m.appendSystem("✓ copied last answer to clipboard")
			}
			return m, nil
		case "ctrl+y":
			// copy last code block
			if code := m.lastCodeBlock(); code != "" {
				_ = copyToClipboard(code)
				m.appendSystem("✓ copied last code block to clipboard")
			} else {
				m.appendSystem("No code block to copy")
			}
			return m, nil
		case "enter":
			if m.streaming {
				text := strings.TrimSpace(m.textarea.Value())
				if text != "" {
					m.engine.Steering.Steer(expandSlashForSteer(m, text))
					m.appendUser(text + " (steered)")
					m.textarea.Reset()
				}
				return m, nil
			}
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.textarea.Reset()
			// bash mode `! ...` — run without LLM; `!! ...` — run then feed output to LLM
			if strings.HasPrefix(text, "!") {
				withLLM := strings.HasPrefix(text, "!!")
				cmd := strings.TrimSpace(strings.TrimLeft(text, "!"))
				if cmd != "" {
					m.appendUser(text)
					if withLLM {
						return m, m.runBashAndAsk(cmd)
					}
					m.runBash(cmd)
				}
				return m, nil
			}
			// slash command intercept
			if strings.HasPrefix(text, "/") {
				handled, cmd, expandPrompt := handleSlash(m, text)
				if handled {
					if expandPrompt == "" {
						return m, cmd
					}
					// prompt template: show original input, send expanded
					m.appendUser(text + m.attachPendingImages())
					m.streaming = true
					m.streamBuffer.Reset()
					m.errMsg = ""
					return m, tea.Batch(m.spinner.Tick, m.runTurn(expandPrompt))
				}
			}
			m.appendUser(text + m.attachPendingImages())
			m.streaming = true
			m.streamBuffer.Reset()
			m.errMsg = ""
			return m, tea.Batch(
				m.spinner.Tick,
				m.runTurn(expandFileRefs(text)),
			)
		}

	case contentDeltaMsg:
		m.streamBuffer.WriteString(string(msg))
		m.refreshViewport()
		return m, nil

	case toolStartMsg:
		m.refreshViewport()
		return m, nil

	case toolEndMsg:
		m.refreshViewport()
		return m, nil

	case turnDoneMsg:
		m.streaming = false
		if msg.Err != nil && msg.Err != context.Canceled {
			m.errMsg = msg.Err.Error()
			m.appendSystem("error: " + msg.Err.Error())
		} else {
			if m.engine != nil && m.engine.Config.SessionTree != nil {
				m.history = m.engine.Config.SessionTree.GetLinearHistory("")
				m.invalidateChatCache()
			}
			m.streamBuffer.Reset()
			m.refreshViewport()
			if last := m.lastAssistantText(); last != "" {
				_ = copyToClipboard(last)
				m.tokens = len(last) / 4
			}
		}
		m.autoSaveSession()
		m.viewport.GotoBottom()
		return m, nil

	case providerModelsFetchedMsg:
		if msg.Err != nil {
			m.appendSystem(errorStyle.Render(fmt.Sprintf("Models for %s: %v", msg.Provider, msg.Err)))
		} else if len(msg.Models) > 0 {
			m.appendSystem(fmt.Sprintf("✓ %s: %d model(s) discovered and cached for /model", msg.Provider, len(msg.Models)))
		}
		// Refresh an open scoped model picker so results appear immediately.
		if m.picker.active && !m.picker.prompt && m.picker.scope != "" &&
			strings.EqualFold(m.picker.scope, msg.Provider) && msg.Err == nil && len(msg.Models) > 0 {
			m.openModelPickerScoped(m.picker.scope, "")
		}
		return m, nil
	case statusMsg:
		m.appendSystem(string(msg))
		return m, nil

	case StatusNotice:
		m.appendSystem(string(msg))
		return m, nil

	case pasteMsg:
		if msg.err != nil {
			m.appendSystem(errorStyle.Render("paste: " + msg.err.Error()))
			return m, nil
		}
		m.pendingImgs = append(m.pendingImgs, *msg.img)
		total := 0
		for _, im := range m.pendingImgs {
			total += len(im.Data) * 3 / 4
		}
		m.appendSystem(fmt.Sprintf("✓ image attached (%s, ~%d KB) — %d pending; send a message to include it",
			msg.img.MediaType, total>>10, len(m.pendingImgs)))
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(tiCmd, vpCmd)
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	header := renderHeader(m.width, m.engine.Config.Model)

	// status line — must stay exactly one terminal row: an untruncated block
	// wraps, blockRows under-counts, and the overflow scrolls the header off
	// screen. Errors live in the transcript (appendSystem), not here, so they
	// are never duplicated.
	status := ""
	if m.streaming {
		status = spinnerStyle.Render(m.spinner.View()) + statusStyle.Render(" Pi is thinking…  (Enter to steer · Ctrl+C to interrupt)")
	} else if m.showHelp {
		status = helpStyle.Render("enter: send · ctrl+c: quit · ctrl+l: clear · y: copy · ctrl+y: copy code · ctrl+p: palette · ctrl+o: model · ctrl+t: think · ↑/↓+Enter: pick")
	}
	if status != "" {
		status = truncateToWidth(status, m.width, "…")
	}

	input := m.textarea.View()
	if m.picker.active {
		hint := "↑/↓ move · Enter select · Esc cancel"
		if m.picker.onDefault != nil {
			hint = "↑/↓ move · Enter select · Ctrl+D select as default · Esc cancel"
		}
		input = helpStyle.Render(hint)
	}

	// footer
	provName := ""
	if m.engine != nil && m.engine.Config.Provider != nil {
		provName = m.engine.Config.Provider.Name()
	}
	cwd, _ := os.Getwd()
	bgLabels := make([]string, 0, len(m.bgTasks))
	for _, l := range m.bgTasks {
		bgLabels = append(bgLabels, l)
	}
	footerData := FooterData{
		Cwd:           cwd,
		Branch:        m.gitBranch,
		SessionName:   m.sessionName,
		Model:         m.engine.Config.Model,
		Provider:      provName,
		ThinkingLevel: m.thinkingMode,
		BgTasks:       bgLabels,
	}
	footer := renderFooter(footerData, m.width)

	// inline slash suggestion popup (shown while typing "/..." with no args)
	slashPopup := ""
	if !m.picker.active && !m.streaming {
		if sugs := m.slashSuggestions(m.textarea.Value()); len(sugs) > 0 {
			cursor := m.slashCursor
			if cursor < 0 {
				cursor = 0
			}
			cursor %= len(sugs)
			const maxRows = 8
			lo := 0
			hi := len(sugs)
			if cursor >= maxRows {
				lo = cursor - maxRows + 1
			}
			if hi > lo+maxRows {
				hi = lo + maxRows
			}
			var sb strings.Builder
			for i, s := range sugs[lo:hi] {
				name := "/" + s.Name
				pad := ""
				if w := 18 - len(name); w > 0 {
					pad = strings.Repeat(" ", w)
				}
				line := name + pad + s.Description
				if w := m.width - 2; w > 0 {
					line = truncateToWidth(line, w, "…")
				}
				if lo+i == cursor {
					sb.WriteString(suggestionActiveStyle.Render(line) + "\n")
				} else {
					sb.WriteString(suggestionStyle.Render(line) + "\n")
				}
			}
			sb.WriteString(helpStyle.Render("↑/↓ navigate · Tab complete · Enter send"))
			slashPopup = sb.String()
		}
	}

	// The viewport gets every row the other blocks don't need; popup and
	// status line shrink it instead of pushing the frame past the screen.
	vp := m.viewport
	vp.Height = max(3, m.height-blockRows(header, status, input, slashPopup, footer))
	mainView := vp.View()
	if m.picker.active {
		if m.picker.prompt {
			shown := m.picker.promptValue
			if m.picker.maskInput {
				shown = strings.Repeat("*", len([]rune(shown)))
			}
			lines := []string{m.picker.title, "", m.picker.promptLabel, "> " + shown,
				"", "Enter submit · Esc cancel"}
			mainView = pickerBoxStyle.Width(min(m.width-6, 64)).Render(strings.Join(lines, "\n"))
		} else {
			var qView string
			if m.picker.searchable {
				qView = m.picker.query.View()
			}
			mainView = renderPickerAnnotated(m.picker.title, m.picker.items, m.picker.cursor, m.width, qView, m.picker.annotations, m.picker.onDefault != nil)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		mainView,
		status,
		input,
		slashPopup,
		footer,
	)
}

// blockRows counts the terminal lines a set of rendered blocks occupies
// (JoinVertical puts each empty block on its own line too).
func blockRows(blocks ...string) int {
	n := 0
	for _, b := range blocks {
		if b == "" {
			n++ // JoinVertical still emits a line for empty blocks
			continue
		}
		n += strings.Count(strings.TrimRight(b, "\n"), "\n") + 1
	}
	return n
}

// attachPendingImages moves Ctrl+V clipboard images onto the engine so the
// next user message carries them, and returns a short label for the bubble.
func (m *Model) attachPendingImages() string {
	if len(m.pendingImgs) == 0 {
		return ""
	}
	m.engine.PendingImages = append(m.engine.PendingImages, m.pendingImgs...)
	n := len(m.pendingImgs)
	m.pendingImgs = nil
	return fmt.Sprintf(" [+%d image%s]", n, map[bool]string{true: "s", false: ""}[n > 1])
}

// appendUser adds a user message to history + viewport using Pi-style bubble (Box bg, no border)
func (m *Model) appendUser(text string) {
	m.history = append(m.history, types.NewUserMessage(text))
	m.chatCache = append(m.chatCache, renderUserBubble(text, m.width))
	m.scrollChatToBottom()
}

// AppendStartupNotice records a startup-time notice (e.g. self-update
// result, MCP status) into the chat log. Called from main before the program runs.
func (m *Model) AppendStartupNotice(text string) { m.appendSystem(text) }

// appendSystem adds a display-only notice line to the chat log. Notices are
// NOT model history (they never reach the LLM or /export) but survive full
// re-renders via their history-size anchor.
func (m *Model) appendSystem(text string) {
	m.appendRendered(statusStyle.Render(text))
}

// appendRendered adds a pre-rendered display-only chat entry (notices, bash
// results, etc.) anchored at the current history size.
func (m *Model) appendRendered(rendered string) {
	m.notices = append(m.notices, chatNotice{anchor: len(m.history), text: rendered})
	m.chatCache = append(m.chatCache, strings.TrimRight(rendered, "\n")+"\n")
	m.scrollChatToBottom()
}

// invalidateChatCache marks the cache stale — the next rebuild renders every
// history entry fresh (after reload/fork/compact/clear or a tree sync).
func (m *Model) invalidateChatCache() { m.chatStale = true }

// scrollChatToBottom re-syncs the viewport with the chat cache. The view only
// follows the bottom when the user was already there — manual scroll-back is
// preserved while new entries arrive. Bubbletea keeps trailing newlines as
// scrollable blank rows, so we trim them first.
func (m *Model) scrollChatToBottom() {
	m.rebuildChatCache()
	wasBottom := m.viewport.AtBottom()
	m.viewport.SetContent(strings.TrimRight(m.chatContent(), "\n"))
	if wasBottom {
		m.viewport.GotoBottom()
	}
}

// chatContent assembles the chat log from the per-entry cache. Entries are
// rendered once per width; streaming text is appended live without caching.
func (m *Model) chatContent() string {
	var sb strings.Builder
	for _, s := range m.chatCache {
		sb.WriteString(s)
		if !strings.HasSuffix(s, "\n") {
			sb.WriteString("\n")
		}
	}
	if m.streamBuffer.Len() > 0 {
		sb.WriteString(renderAssistantText(m.streamBuffer.String()))
	}
	return sb.String()
}

// refreshViewport rebuilds the viewport content from history with Pi-style rendering:
// user messages → bubble (Box bg), assistant → plain markdown, tools → bubble by state
func (m *Model) refreshViewport() {
	m.rebuildChatCache()
	m.viewport.SetContent(strings.TrimRight(m.chatContent(), "\n"))
	m.viewport.GotoBottom()
}

// rebuildChatCache re-renders every entry when the width changed or the cache
// was invalidated / drifted out of sync (reload, fork, compact, tree sync).
func (m *Model) rebuildChatCache() {
	if !m.chatStale && m.chatCacheWidth == m.width && len(m.chatCache) == len(m.history)+len(m.notices) {
		return
	}
	m.chatCache = make([]string, 0, len(m.history)+len(m.notices))
	m.chatCacheWidth = m.width
	m.chatStale = false
	ni := 0
	for i := 0; i <= len(m.history); i++ {
		// flush notices anchored before history entry i
		for ni < len(m.notices) && m.notices[ni].anchor <= i {
			m.chatCache = append(m.chatCache, strings.TrimRight(m.notices[ni].text, "\n")+"\n")
			ni++
		}
		if i < len(m.history) {
			m.chatCache = append(m.chatCache, renderHistoryEntry(m.history, i, m.width))
		}
	}
}

// renderHistoryEntry renders one message to its chat-log block.
func renderHistoryEntry(history []types.Message, idx int, width int) string {
	h := history[idx]
	switch h.Role {
	case types.RoleUser:
		return renderUserBubble(h.Content, width) + "\n"
	case types.RoleAssistant:
		var sb strings.Builder
		if h.Content != "" {
			sb.WriteString(renderAssistantText(h.Content) + "\n")
		}
		for _, tc := range h.ToolCalls {
			sb.WriteString(renderToolPending(tc.Function.Name, tc.Function.Arguments) + "\n")
		}
		return sb.String()
	case types.RoleTool:
		var sb strings.Builder
		for _, tr := range h.ToolResults {
			toolName := ""
			// find matching tool call (search backwards)
			for i := idx - 1; i >= 0; i-- {
				if history[i].Role == types.RoleAssistant {
					for _, tc := range history[i].ToolCalls {
						if tc.ID == tr.ToolCallID {
							toolName = tc.Function.Name
							break
						}
					}
					if toolName != "" {
						break
					}
				}
			}
			if toolName == "" {
				toolName = "tool"
			}
			body := truncate(tr.Content, 300)
			sb.WriteString(renderToolBubble(toolName, body, tr.IsError) + "\n")
		}
		return sb.String()
	case types.RoleSystem:
		return statusStyle.Render(h.Content) + "\n"
	}
	return ""
}

func (m *Model) lastAssistantText() string {
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].Role == types.RoleAssistant && m.history[i].Content != "" {
			return m.history[i].Content
		}
	}
	if m.streamBuffer.Len() > 0 {
		return m.streamBuffer.String()
	}
	return ""
}

func (m *Model) lastCodeBlock() string {
	text := m.lastAssistantText()
	if text == "" {
		return ""
	}
	// find last ``` fenced code block
	lines := strings.Split(text, "\n")
	inCode := false
	var block []string
	var last []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				if len(block) > 0 {
					last = block
				}
				block = nil
				inCode = false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			block = append(block, l)
		}
	}
	return strings.Join(last, "\n")
}

func (m *Model) cycleThinking() {
	order := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
	next := "medium"
	for i, v := range order {
		if v == m.thinkingMode {
			next = order[(i+1)%len(order)]
			break
		}
	}
	m.setThinking(next)
}

// setThinking updates the TUI label and the engine config in one place.
func (m *Model) setThinking(level string) {
	m.thinkingMode = level
	m.engine.Config.ThinkingLevel = level
	m.appendSystem("Thinking: " + level)
}

// contextWindowForModel returns a best-effort context window size used for
// the footer's usage percentage. Unknown models return 0 (indicator hidden).
func contextWindowForModel(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gemini"):
		return 1_000_000
	case strings.Contains(m, "claude"):
		if strings.Contains(m, "sonnet-4") || strings.Contains(m, "opus") {
			return 200_000
		}
		return 200_000
	case strings.Contains(m, "gpt-4.1"), strings.Contains(m, "o3"), strings.Contains(m, "o4"):
		return 200_000
	case strings.Contains(m, "gpt-4o"), strings.Contains(m, "gpt-5"):
		return 128_000
	case strings.Contains(m, "llama-3"), strings.Contains(m, "qwen2.5"), strings.Contains(m, "deepseek"):
		return 128_000
	case strings.Contains(m, "gpt-4.1-nano"), strings.Contains(m, "haiku"):
		return 200_000
	}
	return 0
}

// runBash executes a `!` command and shows result in a bash bubble.
func (m *Model) runBash(command string) {
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	m.appendRendered(renderBashResult(command, strings.TrimRight(string(out), "\n"), exit, err != nil))
}

// runBashAndAsk executes a `!!` command, shows its output, then sends the
// output to the LLM as context for the next turn.
func (m *Model) runBashAndAsk(command string) tea.Cmd {
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.CombinedOutput()
	output := strings.TrimRight(string(out), "\n")
	if len(output) > 8000 {
		output = output[:4000] + "\n…[truncated]…\n" + output[len(output)-4000:]
	}
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	m.appendRendered(renderBashResult(command, output, exit, err != nil))
	prompt := fmt.Sprintf("I ran the shell command `%s` (exit %d). Its output:\n\n```\n%s\n```\n\nExplain/analyze the output.", command, exit, output)
	m.streaming = true
	m.streamBuffer.Reset()
	m.errMsg = ""
	return tea.Batch(m.spinner.Tick, m.runTurn(prompt))
}

// atRefRe matches @file references in user input (Pi @-mention feature).
var atRefRe = regexp.MustCompile(`(^|\s)@([^\s@]{1,200})`)

// expandFileRefs replaces @path tokens with the file content inline so the
// model actually sees referenced files. Directories and unreadable/oversized
// paths are left as-is.
func expandFileRefs(text string) string {
	cwd, _ := os.Getwd()
	return atRefRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := atRefRe.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		path := parts[2]
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		fi, err := os.Stat(abs)
		if err != nil || fi.IsDir() || fi.Size() > 256*1024 {
			return match
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return match
		}
		return fmt.Sprintf("%s@%s\n```\n%s\n```", parts[1], path, string(data))
	})
}

// generic picker core. When searchable=true, an inline text input is rendered above
// the list; typing filters by case-insensitive substring match; Esc clears the query
// first, then closes the picker on a second press.
func (m *Model) openPicker(title string, items []string, onConfirm func(string) tea.Cmd, searchable bool) {
	if len(items) == 0 {
		items = []string{"(no items)"}
	}
	q := textinput.New()
	q.Placeholder = "type to filter…"
	q.Prompt = "🔎 "
	q.CharLimit = 64
	q.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	q.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8abeb7")).Bold(true)
	q.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	if searchable {
		q.Focus()
	}
	m.picker = pickerState{
		active:     true,
		title:      title,
		items:      append([]string(nil), items...),
		baseItems:  append([]string(nil), items...),
		cursor:     0,
		onConfirm:  onConfirm,
		searchable: searchable,
		query:      q,
		queryEmpty: true,
	}
}

// recomputePicker applies the current query to baseItems and resets cursor.
func (m *Model) recomputePicker() {
	q := strings.ToLower(strings.TrimSpace(m.picker.query.Value()))
	if q == "" {
		m.picker.items = append([]string(nil), m.picker.baseItems...)
	} else {
		filtered := make([]string, 0, len(m.picker.baseItems))
		for _, it := range m.picker.baseItems {
			if strings.Contains(strings.ToLower(it), q) {
				filtered = append(filtered, it)
			}
		}
		if len(filtered) == 0 {
			filtered = []string{"(no matches)"}
		}
		m.picker.items = filtered
	}
	if m.picker.cursor >= len(m.picker.items) {
		m.picker.cursor = 0
	}
}

func (m *Model) filterItems(items []string, filter string) []string {
	if strings.TrimSpace(filter) == "" {
		return items
	}
	f := strings.ToLower(strings.TrimSpace(filter))
	var out []string
	for _, it := range items {
		if strings.Contains(strings.ToLower(it), f) {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return items
	}
	return out
}

func (m *Model) openModelPicker(filter string) {
	m.openModelPickerScoped("", filter)
}

// openModelPickerScoped opens the model list restricted to one provider
// (empty provider = all). filter is a substring narrowing within that scope.
func (m *Model) openModelPickerScoped(provider, filter string) {
	all := m.availableModels()
	var items []string
	for _, full := range all {
		if provider != "" {
			i := strings.Index(full, "/")
			if i <= 0 || !strings.EqualFold(full[:i], provider) {
				continue
			}
		}
		items = append(items, full)
	}
	items = m.filterItems(items, filter)
	if len(items) == 0 {
		if provider != "" {
			items = []string{"(querying " + provider + "…)"}
		} else {
			items = []string{"openai/gpt-4o", "openai/gpt-4o-mini", "anthropic/claude-3-5-sonnet", "gemini/gemini-1.5-flash", "ollama/llama3"}
		}
	}
	cur := ""
	if m.engine != nil {
		cur = m.engine.Config.Model
		if m.engine.Config.Provider != nil && !strings.Contains(cur, "/") {
			cur = m.engine.Config.Provider.Name() + "/" + cur
		}
	}
	cursor := 0
	for i, it := range items {
		if strings.EqualFold(it, cur) {
			cursor = i
			break
		}
	}
	title := "Select model — " + cur
	if provider != "" {
		title = "Select model (" + provider + ") — " + cur
	}
	m.openPicker(title, items, func(sel string) tea.Cmd {
		if strings.HasPrefix(sel, "(querying") {
			return nil
		}
		sel = strings.ReplaceAll(sel, " ", "/")
		m.applyModelSelection(sel)
		return nil
	}, true)
	// Ctrl+D: select the highlighted model AND save it as the default
	// (settings.json defaultProvider/defaultModel), like /model <id> --default.
	m.picker.onDefault = func(sel string) tea.Cmd {
		if strings.HasPrefix(sel, "(querying") {
			return nil
		}
		sel = strings.ReplaceAll(sel, " ", "/")
		m.applyModelSelection(sel + " --default")
		return nil
	}
	m.picker.scope = strings.ToLower(provider)
	m.picker.cursor = cursor
	notes := map[string]string{}
	for _, it := range items {
		if info, ok := modelcatalog.LookupFull(it); ok {
			notes[it] = modelcatalog.FormatSuffix(info, true)
		}
	}
	m.picker.annotations = notes
}

// openModelProviderPicker lists providers the user actually has: everything
// with credentials in auth.json plus everything present in the model catalog
// (models-store.json / cache / favorites). Selecting a provider opens only
// its models; providers with no cached models trigger a live /models fetch.
func (m *Model) openModelProviderPicker() {
	provs := map[string]bool{}
	for _, full := range m.availableModels() {
		if i := strings.Index(full, "/"); i > 0 {
			provs[strings.ToLower(full[:i])] = true
		}
	}
	if saved, err := loadPiAuthMap(); err == nil {
		for p := range saved {
			provs[strings.ToLower(p)] = true
		}
	}
	list := make([]string, 0, len(provs))
	for p := range provs {
		list = append(list, p)
	}
	sort.Strings(list)
	const allItem = "All providers"
	items := append([]string{allItem}, list...)
	m.openPicker("Select provider", items, func(sel string) tea.Cmd {
		if sel == allItem {
			m.openModelPickerScoped("", "")
			return nil
		}
		m.openModelPickerScoped(sel, "")
		// If the provider has no models in the catalog yet, fetch them from
		// its endpoint; the picker refreshes when the results arrive.
		for _, full := range m.availableModels() {
			if i := strings.Index(full, "/"); i > 0 && strings.EqualFold(full[:i], sel) {
				return nil
			}
		}
		m.appendSystem("No cached models for " + sel + " — querying endpoint…")
		return fetchProviderModelsCmd(sel)
	}, false)
}

func (m *Model) confirmPicker() tea.Cmd {
	if !m.picker.active || len(m.picker.items) == 0 {
		m.picker.active = false
		return nil
	}
	sel := m.picker.items[m.picker.cursor]
	cb := m.picker.onConfirm
	m.picker.active = false
	if cb != nil {
		// Panggil sinkron: closure mutate *Model milik Update saat ini.
		// Kalau dikembalikan sebagai tea.Cmd, bubbletea mengeksekusinya
		// ASINKRON di salinan Model yang sudah mati -> pilihan picker hilang.
		return cb(sel)
	}
	return nil
}

// confirmDefaultPicker handles Ctrl+D: like confirmPicker but routes to the
// optional onDefault hook (model picker: select + save as default). Pickers
// without the hook ignore the key entirely.
func (m *Model) confirmDefaultPicker() tea.Cmd {
	if !m.picker.active || len(m.picker.items) == 0 || m.picker.onDefault == nil {
		return nil
	}
	sel := m.picker.items[m.picker.cursor]
	cb := m.picker.onDefault
	m.picker.active = false
	return cb(sel)
}

func (m *Model) applyModelSelection(modelArg string) {
	handleModel(m, modelArg)
}

func (m *Model) openCommandPicker(filter string) {
	var items []string
	for name, c := range slashCommands {
		label := "/" + name + " — " + c.Description
		items = append(items, label)
	}
	// include prompt templates as /name (args)
	if m.engine != nil && m.engine.Config.PromptLoader != nil {
		for _, tpl := range m.engine.Config.PromptLoader.LoadAll() {
			desc := tpl.Description
			if desc == "" {
				desc = "prompt template"
			}
			items = append(items, "/"+tpl.Name+" — "+desc)
		}
	}
	sort.Strings(items)
	items = m.filterItems(items, filter)
	m.openPicker("Select command", items, func(sel string) tea.Cmd {
		cmd := strings.Fields(sel)[0]
		if idx := strings.Index(sel, " —"); idx > 0 {
			cmd = strings.TrimSpace(sel[:idx])
		}
		handled, _, expandPrompt := handleSlash(m, cmd)
		if handled && expandPrompt != "" {
			m.appendUser(cmd)
			m.streaming = true
			m.streamBuffer.Reset()
			m.errMsg = ""
			m.textarea.SetValue("")
			return m.runTurn(expandPrompt)
		}
		m.textarea.SetValue(cmd + " ")
		m.appendSystem("Selected " + cmd + " — type arg then Enter, or Enter again to open picker list")
		return nil
	}, true)
}

func (m *Model) availableModels() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range loadFavorites() {
		if !seen[strings.ToLower(f)] {
			out = append(out, f)
			seen[strings.ToLower(f)] = true
		}
	}
	// models discovered via /models endpoint (custom providers, /login fetch)
	for prov, mods := range loadModelsCache() {
		for _, mod := range mods {
			full := prov + "/" + mod
			if !seen[strings.ToLower(full)] {
				out = append(out, full)
				seen[strings.ToLower(full)] = true
			}
		}
	}
	if dir := piAgentDirTUI(); dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, "models-store.json")); err == nil {
			var raw map[string]struct {
				Models []struct {
					ID  string `json:"id"`
					API string `json:"api"`
				} `json:"models"`
			}
			if json.Unmarshal(data, &raw) == nil {
				for prov, entry := range raw {
					for _, mod := range entry.Models {
						if mod.API == "openai-responses" {
							continue
						}
						full := prov + "/" + mod.ID
						if !seen[strings.ToLower(full)] {
							out = append(out, full)
							seen[strings.ToLower(full)] = true
						}
					}
				}
			}
		}
	}
	// Embedded curated catalog (models.dev snapshot): adds real models +
	// metadata for providers the user has credentials for but which are
	// missing from models-store.json (e.g. anthropic, openai, xai). On a
	// fresh install with nothing else, the whole catalog is listed so the
	// picker is never empty.
	haveProv := map[string]bool{}
	if saved, err := loadPiAuthMap(); err == nil {
		for pr := range saved {
			haveProv[strings.ToLower(pr)] = true
		}
	}
	for _, full := range modelcatalog.CatalogModels() {
		pr := full
		if i := strings.Index(full, "/"); i > 0 {
			pr = full[:i]
		}
		if len(out) > 0 && !haveProv[pr] {
			continue
		}
		if !seen[full] {
			out = append(out, full)
			seen[full] = true
		}
	}
	if len(out) == 0 {
		out = []string{"openai/gpt-4o", "openai/gpt-4o-mini", "anthropic/claude-3-5-sonnet", "gemini/gemini-1.5-flash", "ollama/llama3"}
	}
	return out
}

func (m Model) runTurn(prompt string) tea.Cmd {
	engine := m.engine
	ctx := m.ctx
	return func() tea.Msg {
		var buf strings.Builder
		capture := &captureListener{buf: &buf, model: m}
		orig := engine.Config.Listener
		engine.Config.Listener = &chainedTUIListener{primary: capture, secondary: orig}
		err := engine.RunTurn(ctx, prompt)
		engine.Config.Listener = orig
		_ = buf.String()
		return turnDoneMsg{Err: err}
	}
}

// captureListener forwards events to the TUI as messages for live rendering.
type captureListener struct {
	buf   *strings.Builder
	model Model
}

func (l *captureListener) OnTurnStart()                {}
func (l *captureListener) OnTurnEnd()                  {}
func (l *captureListener) OnContentDelta(delta string) { l.buf.WriteString(delta) }
func (l *captureListener) OnToolExecutionStart(toolName string, argsJSON string) {
	// toolStartMsg is sent via tea program; we can't call tea here from arbitrary goroutine,
	// so the engine caller (model.runTurn) handles appending via session tree -> refreshViewport
}
func (l *captureListener) OnToolExecutionEnd(toolName string, result string, isError bool) {
}
func (l *captureListener) OnUsage(usage types.TokenUsage) {
	if l.model.usage == nil {
		return
	}
	l.model.usage.InputTokens += usage.PromptTokens
	l.model.usage.OutputTokens += usage.CompletionTokens
	l.model.usage.ContextUsed = usage.PromptTokens
	l.model.usage.CacheRead += usage.CacheReadTokens
	l.model.usage.CacheWrite += usage.CacheWriteTokens
}
func (l *captureListener) OnError(err error) {}

// chainedTUIListener forwards to both capture and original
type chainedTUIListener struct {
	primary   agent.EventListener
	secondary agent.EventListener
}

func (c *chainedTUIListener) OnTurnStart() {
	if c.primary != nil {
		c.primary.OnTurnStart()
	}
	if c.secondary != nil {
		c.secondary.OnTurnStart()
	}
}
func (c *chainedTUIListener) OnTurnEnd() {
	if c.primary != nil {
		c.primary.OnTurnEnd()
	}
	if c.secondary != nil {
		c.secondary.OnTurnEnd()
	}
}
func (c *chainedTUIListener) OnContentDelta(delta string) {
	if c.primary != nil {
		c.primary.OnContentDelta(delta)
	}
	if c.secondary != nil {
		c.secondary.OnContentDelta(delta)
	}
}
func (c *chainedTUIListener) OnUsage(usage types.TokenUsage) {
	if c.primary != nil {
		c.primary.OnUsage(usage)
	}
	if c.secondary != nil {
		c.secondary.OnUsage(usage)
	}
}
func (c *chainedTUIListener) OnToolExecutionStart(tool string, args string) {
	if c.primary != nil {
		c.primary.OnToolExecutionStart(tool, args)
	}
	if c.secondary != nil {
		c.secondary.OnToolExecutionStart(tool, args)
	}
}
func (c *chainedTUIListener) OnToolExecutionEnd(tool string, result string, isError bool) {
	if c.primary != nil {
		c.primary.OnToolExecutionEnd(tool, result, isError)
	}
	if c.secondary != nil {
		c.secondary.OnToolExecutionEnd(tool, result, isError)
	}
}
func (c *chainedTUIListener) OnError(err error) {
	if c.primary != nil {
		c.primary.OnError(err)
	}
	if c.secondary != nil {
		c.secondary.OnError(err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
