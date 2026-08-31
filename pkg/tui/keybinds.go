package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines shortcuts for the Pi TUI
type KeyMap struct {
	Quit       key.Binding
	Interrupt  key.Binding
	Clear      key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Send       key.Binding
	Steer      key.Binding
	ToggleHelp key.Binding
	Copy       key.Binding
	CopyCode   key.Binding
	Palette    key.Binding
	ModelPick  key.Binding
	ThinkCycle key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "ctrl+q"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Interrupt: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "interrupt"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdown", "scroll down"),
		),
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Steer: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "steer (while streaming)"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Copy: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy last answer"),
		),
		CopyCode: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("ctrl+y", "copy code"),
		),
		Palette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "command palette"),
		),
		ModelPick: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "model picker"),
		),
		ThinkCycle: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "cycle thinking"),
		),
	}
}
