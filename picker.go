package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type ServicePicker struct {
	model *PickerModel
}

type PickerModel struct {
	services        []string
	visible         []string // services matching the filter, best match first
	selected        int      // index into visible
	filter          string
	filtering       bool // the filter prompt has focus
	width           int
	height          int
	viewport        viewport.Model
	ready           bool
	err             string
	selectedService string
	targets         map[string]string // service -> "host:port/db", read when the list loads
	help            help.Model
	showHelp        bool
}

// pickerReloadMsg carries the service list re-read after editing the config
type pickerReloadMsg struct {
	services []string
	err      error
}

func NewServicePicker() *ServicePicker {
	m := &PickerModel{help: help.New()}
	services, err := listServices()
	if err != nil {
		m.err = fmt.Sprintf("Failed to load services: %v", err)
	}
	m.setServices(services)
	return &ServicePicker{model: m}
}

func (sp *ServicePicker) Run() (string, error) {
	p := tea.NewProgram(sp.model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return "", fmt.Errorf("failed to run service picker: %w", err)
	}
	return sp.model.selectedService, nil
}

func (m *PickerModel) Init() tea.Cmd {
	return nil
}

func (m *PickerModel) setServices(services []string) {
	m.services = services
	m.targets = make(map[string]string, len(services))
	for _, s := range services {
		m.targets[s] = serviceTarget(s)
	}
	m.applyFilter()
}

// applyFilter recomputes the visible services, keeping the selection on the
// same service when it still matches
func (m *PickerModel) applyFilter() {
	current := ""
	if m.selected < len(m.visible) {
		current = m.visible[m.selected]
	}

	m.visible = m.visible[:0]
	for _, i := range fuzzyRank(m.filter, len(m.services), func(i int) string { return m.services[i] }) {
		m.visible = append(m.visible, m.services[i])
	}

	m.selected = 0
	if m.filter == "" {
		for i, s := range m.visible {
			if s == current {
				m.selected = i
			}
		}
	}
}

func (m *PickerModel) moveSelection(delta int) {
	if len(m.visible) == 0 {
		return
	}
	m.selected = max(0, min(len(m.visible)-1, m.selected+delta))
}

func (m *PickerModel) choose() (tea.Model, tea.Cmd) {
	if m.selected < len(m.visible) {
		m.selectedService = m.visible[m.selected]
		return m, tea.Quit
	}
	return m, nil
}

func (m *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if !m.ready || msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		for i := range m.visible {
			if zone.Get(fmt.Sprintf("service_%d", i)).InBounds(msg) {
				m.selected = i
				return m.choose()
			}
		}
		return m, nil

	case pickerReloadMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.err = ""
			m.setServices(msg.services)
		}
		m.updateContent()
		return m, nil

	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}
		if m.filtering {
			return m.handleFilterKey(msg)
		}

		switch msg.String() {
		case "?":
			m.showHelp = !m.showHelp
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.filter != "" {
				m.filter = ""
				m.applyFilter()
			} else {
				return m, tea.Quit
			}
		case "/":
			m.filtering = true
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "enter", " ":
			return m.choose()
		case "e":
			return m, m.editConfig()
		}
		m.updateContent()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width, max(1, msg.Height-1)) // the last line is the key-hint bar
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = max(1, msg.Height-1)
		}

		m.updateContent()
	}

	return m, nil
}

// handleFilterKey edits the filter while the prompt has focus. Arrow keys
// still move the selection so a match can be picked without leaving it.
func (m *PickerModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEscape:
		m.filtering = false
		m.filter = ""
		m.applyFilter()
	case tea.KeyEnter:
		return m.choose()
	case tea.KeyUp:
		m.moveSelection(-1)
	case tea.KeyDown:
		m.moveSelection(1)
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.applyFilter()
		}
	case tea.KeyRunes, tea.KeySpace:
		m.filter += string(msg.Runes)
		m.applyFilter()
	}
	m.updateContent()
	return m, nil
}

// editConfig opens ~/.pg_service.conf in $EDITOR, then re-reads it. The
// callback only builds a message; Update applies it.
func (m *PickerModel) editConfig() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, os.ExpandEnv("$HOME/.pg_service.conf"))
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return pickerReloadMsg{err: fmt.Errorf("failed to edit config: %w", err)}
		}
		services, err := listServices()
		if err != nil {
			return pickerReloadMsg{err: fmt.Errorf("failed to reload services: %w", err)}
		}
		return pickerReloadMsg{services: services}
	})
}

func (m *PickerModel) updateContent() {
	var b strings.Builder

	b.WriteString("\n  " + styleBlue.Bold(true).Render("psq") + "  " + styleDim.Render("pick a service from ~/.pg_service.conf") + "\n\n")

	// Show help if requested
	if m.showHelp {
		b.WriteString(m.customHelpView())
		m.viewport.SetContent(b.String())
		return
	}

	switch {
	case m.filtering:
		b.WriteString("  " + styleBlue.Render("/ ") + m.filter + "█\n")
	case m.filter != "":
		b.WriteString("  " + styleBlue.Render("/ ") + m.filter + styleDim.Render("  (esc to clear)") + "\n")
	default:
		b.WriteString("  " + styleDim.Render("/ to filter") + "\n")
	}
	b.WriteString("\n")

	switch {
	case m.err != "":
		b.WriteString("  " + styleRed.Render("Error: "+m.err))
	case len(m.services) == 0:
		b.WriteString("  " + styleDim.Render("No services found in ~/.pg_service.conf") + "\n")
		b.WriteString("  " + styleDim.Render("Press ") + styleKey.Render("e") + styleDim.Render(" to edit the configuration file."))
	case len(m.visible) == 0:
		b.WriteString("  " + styleDim.Render("No services match the filter"))
	default:
		nameW := 12
		for _, s := range m.visible {
			nameW = max(nameW, len([]rune(s))+3)
		}
		for i, service := range m.visible {
			selected := i == m.selected
			marker := "  "
			if selected {
				marker = "▸ "
			}
			row := renderSegs([]seg{
				{" ", styleFg},
				{marker, styleBlue},
				{fitWidth(service, nameW), styleFg.Bold(selected)},
				{m.targets[service], styleDim},
			}, max(20, m.width-2), selected)
			// Wrap in bubblezone mark for clickability
			b.WriteString(" " + zone.Mark(fmt.Sprintf("service_%d", i), row) + "\n")
		}
	}

	m.viewport.SetContent(b.String())
}

// pickerHints are the bottom-bar keys for the service picker
func (m *PickerModel) pickerHints() []keyHint {
	switch {
	case m.showHelp:
		return []keyHint{{"?", "close help"}, {"q", "quit"}}
	case m.filtering:
		return []keyHint{{"type", "to filter"}, {"↑/↓", "move"}, {"enter", "connect"}, {"esc", "clear"}}
	}
	return []keyHint{{"↑/↓", "move"}, {"enter", "connect"}, {"/", "filter"}, {"e", "edit"}, {"?", "help"}, {"q", "quit"}}
}

// serviceTarget describes where a service points: host:port/db, plus the
// sign-in method when it isn't a password
func serviceTarget(service string) string {
	cfg, err := getDBConfig(service)
	if err != nil || cfg.Host == "" {
		return ""
	}
	target := cfg.Host
	if cfg.Port != "" {
		target += ":" + cfg.Port
	}
	if cfg.Database != "" {
		target += "/" + cfg.Database
	}
	if cfg.UsesRDSIAM() {
		target += " · rds-iam"
	}
	return target
}

func (m *PickerModel) customHelpView() string {
	var helpText strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	line := func(key, desc string) {
		helpText.WriteString(keyStyle.Render(key) + " " + descStyle.Render(desc) + "\n")
	}

	helpText.WriteString(titleStyle.Render("Service Picker Help") + "\n\n")

	helpText.WriteString(titleStyle.Render("Navigation:") + "\n")
	line("↑/k", "move up")
	line("↓/j", "move down")
	line("/", "fuzzy filter services (esc clears)")
	line("click", "select service")
	line("enter/space", "select service")
	helpText.WriteString("\n")

	helpText.WriteString(titleStyle.Render("Configuration:") + "\n")
	line("e", "edit ~/.pg_service.conf")
	helpText.WriteString("\n")

	helpText.WriteString(titleStyle.Render("System:") + "\n")
	line("?", "toggle help")
	line("q/esc", "quit")

	return helpText.String()
}

func (m *PickerModel) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if !m.ready {
		return "Getting ready..."
	}

	return zone.Scan(m.viewport.View() + "\n" + renderHints(m.pickerHints(), m.width))
}
