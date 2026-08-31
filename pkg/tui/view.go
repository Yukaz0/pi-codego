package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Pi-style theme — subtle background fills, NO borders (Pi uses Box padding + bg, no border).
// Reference: earendil-works/pi packages/coding-agent/src/modes/interactive/theme/dark.json
// and components/user-message.ts (Box paddingX=1, paddingY=1, bg = userMessageBg)
var (
	// Header
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#8abeb7")).
			Padding(0, 1)

	// Status / muted
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080")).
			Italic(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cc6666")).
			Bold(true)

	// Warning (yellow) — used by footer context% > 70%.
	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffff00"))

	suggestionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9aa5b1")).
			Padding(0, 1)
	suggestionActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e6e6e6")).
				Background(lipgloss.Color("#2d333b")).
				Padding(0, 1)
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	// User message bubble — Box with userMessageBg fill, padding (1,1), no border (matches Pi)
	userBubbleStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#343541")).
			Foreground(lipgloss.Color("#d4d4d4")).
			Padding(1, 1)

	// Assistant text — no bubble, plain markdown with left padding (matches Pi)
	assistantTextStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("#d4d4d4"))

	// Thinking text — italic, dim, left padding (matches Pi assistant thinking)
	thinkingTextStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("#808080")).
				Italic(true)

	// Tool execution bubbles — subtle background fill, no border
	toolPendingStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#282832")).
				Foreground(lipgloss.Color("#d4d4d4")).
				Padding(0, 1)

	toolSuccessStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#283228")).
				Foreground(lipgloss.Color("#d4d4d4")).
				Padding(0, 1)

	toolErrorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3c2828")).
			Foreground(lipgloss.Color("#cc6666")).
			Padding(0, 1)

	// Custom message bubble (bash, branch, etc.)
	customBubbleStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2d2838")).
				Foreground(lipgloss.Color("#9575cd")).
				Padding(1, 1)

	// Bash mode result bubble
	bashResultStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#283228")).
			Foreground(lipgloss.Color("#b5bd68")).
			Padding(0, 1)

	// Bash error
	bashErrorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3c2828")).
			Foreground(lipgloss.Color("#cc6666")).
			Padding(0, 1)

	// Markdown accent colors (matches Pi getMarkdownTheme)
	mdHeadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f0c674")).
			Bold(true)

	mdLinkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#81a2be"))

	mdCodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8abeb7"))

	mdListBulletStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8abeb7"))

	// Footer
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Padding(0, 1)

	footerAccentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8abeb7")).
				Bold(true)

	// Spinner
	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8abeb7"))

	// Model picker
	pickerBoxStyle = lipgloss.NewStyle().
			Padding(1, 2).
			MarginTop(1)

	pickerSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3a3a4a")).
				Foreground(lipgloss.Color("#d4d4d4")).
				Bold(true).
				Padding(0, 1)

	pickerNormalStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d4d4d4")).
				Padding(0, 1)

	pickerHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#8abeb7"))

	// Editor prompt
	editorPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8abeb7")).
				Bold(true)

	// Legacy aliases (used by commands.go and other code)
	toolStyle = toolSuccessStyle
)

// OSC 133 semantic prompt markers (Pi-style, for terminal semantic navigation)
const (
	osc133PromptStart = "\x1b]133;A\x07"
	osc133PromptEnd   = "\x1b]133;B\x07"
	osc133OutputStart = "\x1b]133;B\x07"
	osc133OutputEnd   = "\x1b]133;C\x07"
)

var glamourRenderer *glamour.TermRenderer

func init() {
	// Notty style = plain ANSI, no background fills on headings (matches Pi's no-bubble markdown)
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(100),
	)
	glamourRenderer = r
}

func renderMarkdown(md string) string {
	if glamourRenderer == nil {
		return md
	}
	out, err := glamourRenderer.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimSpace(out)
}

func renderHeader(width int, model string) string {
	title := " ◈ Pi Coding Agent "
	if model != "" {
		title += "· " + model + " "
	}
	bar := strings.Repeat("─", max(0, width-lipgloss.Width(title)-4))
	return headerStyle.Render(title + bar)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncateToWidth — ANSI-aware truncation. If visible width > n, cut to n-elip.
func truncateToWidth(s string, n int, ellipsis string) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if ellipsis == "" {
		ellipsis = "…"
	}
	// crude byte-walk that respects ANSI escape sequences.
	// For footer strings (no nested escapes) this is sufficient.
	var b strings.Builder
	col := 0
	i := 0
	for i < len(s) && col < n-lipgloss.Width(ellipsis) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// CSI sequence — copy until final byte @A-~
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				b.WriteString(s[i : j+1])
				i = j + 1
			} else {
				b.WriteByte(s[i])
				i++
			}
			continue
		}
		// single column
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		col++
		i += size
	}
	b.WriteString(ellipsis)
	return b.String()
}

// renderUserBubble — Pi-style Box: userMessageBg fill, padding 1,1, no border.
// Output wrapped to width so the bg fill extends edge-to-edge inside the box.
func renderUserBubble(text string, width int) string {
	content := renderMarkdown(text)
	if content == "" {
		content = text
	}
	if width > 4 {
		return userBubbleStyle.Width(width - 2).Render(content)
	}
	return userBubbleStyle.Render(content)
}

// renderAssistantText — plain markdown, padding-left only (no bubble, matches Pi).
func renderAssistantText(text string) string {
	content := renderMarkdown(text)
	if content == "" {
		content = text
	}
	return assistantTextStyle.Render(content)
}

// renderThinkingText — italic, dim, padding-left only (matches Pi thinking rendering).
func renderThinkingText(text string) string {
	return thinkingTextStyle.Render(text)
}

// renderToolBubble — Box with state-based background fill, no border (matches Pi Box(1,1,bg)).
// Header: bold tool name. Body: muted output lines. ✓/✗ icon for at-a-glance state.
func renderToolBubble(tool, body string, isError bool) string {
	icon := "✓"
	if isError {
		icon = "✗"
	}
	head := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%s %s", icon, tool))
	style := toolSuccessStyle
	if isError {
		style = toolErrorStyle
	}
	if body == "" {
		return style.Render(head)
	}
	return style.Render(head + "\n" + statusStyle.Render(body))
}

// renderToolPending — Box with toolPendingBg fill, padding 1,1 (no border, matches Pi tool execution).
// "Running..." muted text replaces the static ▶ icon to signal liveness.
func renderToolPending(tool, args string) string {
	head := lipgloss.NewStyle().Bold(true).Render(tool)
	if args != "" {
		head = head + " " + statusStyle.Render(truncate(args, 80))
	}
	status := statusStyle.Render("Running…")
	return toolPendingStyle.Render(head + "\n" + status)
}

// renderCustomBubble — for branchSummary (matches Pi customMessageBg box).
func renderCustomBubble(label, body string) string {
	content := label
	if body != "" {
		content += "\n" + body
	}
	return customBubbleStyle.Render(content)
}

// renderBashResult — Pi-style DynamicBorder bash block: top/bottom border in bashMode color,
// bold "$ cmd" header, muted output. No bg fill (matches reference bash-execution.ts).
func renderBashResult(cmd, output string, exitCode int, isError bool) string {
	borderColor := lipgloss.NewStyle().Foreground(lipgloss.Color("#b5bd68"))
	if isError {
		borderColor = lipgloss.NewStyle().Foreground(lipgloss.Color("#cc6666"))
	}
	width := mcpWidthGuess() // best-effort width; lipgloss will clip per line
	border := borderColor.Render(strings.Repeat("─", max(10, width)))
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#b5bd68")).Render("$ " + cmd)
	if exitCode != 0 && isError {
		head = head + "  " + errorStyle.Render(fmt.Sprintf("(exit %d)", exitCode))
	} else {
		head = head + "  " + dimStyle.Render("✓")
	}
	body := output
	if body == "" {
		body = dimStyle.Render("(no output)")
	} else {
		body = statusStyle.Render(body)
	}
	return border + "\n" + head + "\n" + body + "\n" + border
}

// mcpWidthGuess — heuristic width for border. Best-effort; actual width comes from
// viewport. Borders are clipped to available columns by the terminal.
func mcpWidthGuess() int {
	if w := os.Getenv("COLUMNS"); w != "" {
		if n, err := strconv.Atoi(w); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

func copyToClipboard(text string) error {
	if text == "" {
		return nil
	}
	if err := clipboard.WriteAll(text); err == nil {
		return nil
	}
	return fmt.Errorf("clipboard not available")
}

func renderPicker(title string, items []string, cursor int, width int, queryView string) string {
	if len(items) == 0 {
		return pickerBoxStyle.Render(pickerHeaderStyle.Render("No items"))
	}
	if title == "" {
		title = " Select — ↑/↓ navigate · Enter confirm · Esc cancel "
	}
	var sb strings.Builder
	sb.WriteString(pickerHeaderStyle.Render(" "+title+" ") + "\n")
	if queryView != "" {
		sb.WriteString(queryView + "\n")
		sb.WriteString(helpStyle.Render("type to filter · ↑/↓ navigate · Enter select · Esc clear/close") + "\n\n")
	} else {
		sb.WriteString(helpStyle.Render("↑/↓ navigate · Enter confirm · Esc cancel") + "\n\n")
	}
	start, end := 0, len(items)
	maxShow := 10
	if len(items) > maxShow {
		start = cursor - maxShow/2
		if start < 0 {
			start = 0
		}
		end = start + maxShow
		if end > len(items) {
			end = len(items)
			start = end - maxShow
		}
		if start > 0 {
			sb.WriteString(helpStyle.Render(fmt.Sprintf("  … %d more above", start)) + "\n")
		}
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == cursor {
			prefix = "▸ "
			sb.WriteString(prefix + pickerSelectedStyle.Render(items[i]) + "\n")
		} else {
			sb.WriteString(prefix + pickerNormalStyle.Render(items[i]) + "\n")
		}
	}
	if end < len(items) {
		sb.WriteString(helpStyle.Render(fmt.Sprintf("  … %d more below", len(items)-end)) + "\n")
	}
	sb.WriteString("\n" + helpStyle.Render(fmt.Sprintf("%d/%d", cursor+1, len(items))))
	box := pickerBoxStyle.Width(min(width-6, 64)).Render(sb.String())
	return box
}

func renderModelPicker(items []string, cursor int, width int) string {
	return renderPicker("Select model", items, cursor, width, "")
}

// FooterData — fields the footer needs to render. Mirrors Pi's ReadonlyFooterDataProvider.
type FooterData struct {
	Cwd           string
	Branch        string
	SessionName   string
	Model         string
	Provider      string
	InputTokens   int
	OutputTokens  int
	CacheRead     int
	CacheWrite    int
	ContextWindow int      // 0 = unknown
	ContextUsed   int      // 0 = unknown
	ThinkingLevel string   // "off" | "minimal" | ... | "max"; empty = no indicator
	BgTasks       []string // labels of background tasks
}

func formatTokens(count int) string {
	if count <= 0 {
		return "0"
	}
	if count < 1000 {
		return fmt.Sprintf("%d", count)
	}
	if count < 10000 {
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	}
	if count < 1_000_000 {
		return fmt.Sprintf("%dk", count/1000)
	}
	if count < 10_000_000 {
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	}
	return fmt.Sprintf("%dM", count/1_000_000)
}

func formatCwdForFooter(cwd, home string) string {
	if home == "" || cwd == "" {
		return cwd
	}
	rel, err := filepath.Rel(home, cwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return cwd
	}
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}

// renderFooter — Pi-style 3-line footer:
//
//	line 1: pwd (branch) • sessionName           (dim)
//	line 2: ↑In ↓Out R CR CW stats … (provider) model • thinkingLevel
//	line 3: extension statuses / bg tasks
//
// Context % colored: red > 90, yellow > 70, else default.
func renderFooter(d FooterData, width int) string {
	home, _ := os.UserHomeDir()
	pwd := formatCwdForFooter(d.Cwd, home)
	if d.Branch != "" {
		pwd = pwd + " (" + d.Branch + ")"
	}
	if d.SessionName != "" {
		pwd = pwd + " • " + d.SessionName
	}
	pwdLine := truncateToWidth(pwd, width, "…")
	pwdLine = dimStyle.Render(pwdLine)

	// stats — left side
	var stats []string
	if d.InputTokens > 0 {
		stats = append(stats, "↑"+formatTokens(d.InputTokens))
	}
	if d.OutputTokens > 0 {
		stats = append(stats, "↓"+formatTokens(d.OutputTokens))
	}
	if d.CacheRead > 0 {
		stats = append(stats, "R"+formatTokens(d.CacheRead))
	}
	if d.CacheWrite > 0 {
		stats = append(stats, "W"+formatTokens(d.CacheWrite))
	}
	if d.ContextWindow > 0 {
		pct := 0.0
		if d.ContextUsed > 0 {
			pct = float64(d.ContextUsed) / float64(d.ContextWindow) * 100
		}
		pctStr := fmt.Sprintf("%.1f%%/%s", pct, formatTokens(d.ContextWindow))
		var pctStyled string
		switch {
		case pct > 90:
			pctStyled = errorStyle.Render(pctStr)
		case pct > 70:
			pctStyled = warningStyle.Render(pctStr)
		default:
			pctStyled = pctStr
		}
		stats = append(stats, pctStyled)
	} else if d.InputTokens == 0 && d.OutputTokens == 0 {
		// cheap token estimate from last assistant length (4 chars ≈ 1 token)
		stats = append(stats, dimStyle.Render("0 tok"))
	}
	statsLeft := strings.Join(stats, " ")

	// right side: model (provider) • thinkingLevel
	modelName := d.Model
	if modelName == "" {
		modelName = "no-model"
	}
	right := modelName
	if d.Provider != "" {
		right = "(" + d.Provider + ") " + right
	}
	if d.ThinkingLevel != "" {
		if d.ThinkingLevel == "off" {
			right = right + " • thinking off"
		} else {
			right = right + " • " + d.ThinkingLevel
		}
	}

	// truncate right if needed
	rightWidth := lipgloss.Width(right)
	statsLeftWidth := lipgloss.Width(statsLeft)
	minPad := 2
	if statsLeftWidth+minPad+rightWidth <= width {
		padding := strings.Repeat(" ", width-statsLeftWidth-rightWidth)
		statsLeft = dimStyle.Render(statsLeft)
		right = dimStyle.Render(right)
		statsLine := statsLeft + padding + right
		return pwdLine + "\n" + statsLine + bgTaskLine(d.BgTasks, width, "\n")
	}
	// truncate right
	avail := width - statsLeftWidth - minPad
	if avail > 0 {
		right = truncateToWidth(right, avail, "")
		statsLeft = dimStyle.Render(statsLeft)
		right = dimStyle.Render(right)
		padding := strings.Repeat(" ", max(0, width-statsLeftWidth-lipgloss.Width(right)))
		statsLine := statsLeft + padding + right
		return pwdLine + "\n" + statsLine + bgTaskLine(d.BgTasks, width, "\n")
	}
	return pwdLine + "\n" + dimStyle.Render(statsLeft) + bgTaskLine(d.BgTasks, width, "\n")
}

func bgTaskLine(tasks []string, width int, sep string) string {
	if len(tasks) == 0 {
		return ""
	}
	sorted := make([]string, len(tasks))
	copy(sorted, tasks)
	sort.Strings(sorted)
	line := strings.Join(sorted, " ")
	return sep + dimStyle.Render(truncateToWidth(line, width, "…"))
}

// renderEditorPrompt — "❯" prompt prefix in Pi-style.
func renderEditorPrompt() string {
	return editorPromptStyle.Render("❯")
}
