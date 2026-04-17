// Package keys exposes the application-wide keymap. Every binding is defined
// once here so help and Update() stay in sync.
package keys

import "github.com/charmbracelet/bubbles/key"

type Map struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Back    key.Binding
	Search  key.Binding
	Costs   key.Binding
	Portal  key.Binding
	Detail  key.Binding
	PIM     key.Binding
	Exec    key.Binding
	Refresh key.Binding
	Sort    key.Binding
	Flag    key.Binding
	Palette key.Binding
	Help    key.Binding
	Quit    key.Binding
}

func Default() Map {
	return Map{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter", "l"), key.WithHelp("↵", "drill")),
		Back:    key.NewBinding(key.WithKeys("esc", "h", "backspace"), key.WithHelp("esc", "back")),
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Costs:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "costs")),
		Portal:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "portal")),
		Detail:  key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "info")),
		PIM:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "PIM")),
		Exec:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "exec")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Sort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Flag:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "flag")),
		Palette: key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "palette")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
