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
			m.viewport = viewport.New(msg.Width, msg.Height)
			m.viewport.Style = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height
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
	var content string

	// Header
	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Padding(0, 1).
		Render("psq - Service Picker")
	content += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Press ? for help")
	content += "\n\n"

	// Show help if requested
	if m.showHelp {
		content += m.customHelpView()
		m.viewport.SetContent(content)
		return
	}

	switch {
	case m.filtering:
		content += "Filter: " + m.filter + "█\n"
	case m.filter != "":
		content += fmt.Sprintf("Filter: %s  (esc to clear)\n", m.filter)
	default:
		content += "Select a database service to monitor (/ to filter):\n"
	}

	switch {
	case m.err != "":
		content += "Error: " + m.err
	case len(m.services) == 0:
		content += "No services found in ~/.pg_service.conf\nPress 'e' to edit the configuration file."
	case len(m.visible) == 0:
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No services match the filter")
	default:
		for i, service := range m.visible {
			var serviceText string
			if i == m.selected {
				// Selected service: bold, colored, with background
				style := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("86")).
					Background(lipgloss.Color("235"))
				serviceText = style.Render("▶ " + service)
			} else {
				// Non-selected services: subtle background and padding to show they're clickable
				baseStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("238")).
					Foreground(lipgloss.Color("252"))
				serviceText = baseStyle.Render(service)
			}

			// Wrap in bubblezone mark for clickability
			content += zone.Mark(fmt.Sprintf("service_%d", i), serviceText)
			content += "\n"
		}
	}

	m.viewport.SetContent(content)
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

	return zone.Scan(m.viewport.View())
}
