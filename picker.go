package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type pickerKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Click  key.Binding
	Select key.Binding
	Edit   key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func (k pickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Click, k.Select, k.Edit, k.Help, k.Quit}
}

func (k pickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Click, k.Select},
		{k.Edit, k.Help, k.Quit},
	}
}

var pickerKeys = pickerKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Click: key.NewBinding(
		key.WithKeys(),
		key.WithHelp("click", "select service"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "select service"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit ~/.pg_service.conf"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
		key.WithHelp("esc", "quit"),
	),
}

type ServicePicker struct {
	model *PickerModel
}

type PickerModel struct {
	services        []string
	selected        int
	width           int
	height          int
	viewport        viewport.Model
	ready           bool
	err             string
	selectedService string
	help            help.Model
	showHelp        bool
}

func NewServicePicker() *ServicePicker {
	services, err := listServices()
	if err != nil {
		return &ServicePicker{
			model: &PickerModel{
				services: []string{},
				selected: 0,
				err:      fmt.Sprintf("Failed to load services: %v", err),
				ready:    false,
			},
		}
	}

	return &ServicePicker{
		model: &PickerModel{
			services: services,
			selected: 0,
			ready:    false,
			help:     help.New(),
			showHelp: false,
		},
	}
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

func (m *PickerModel) ensureValidSelection() {
	if len(m.services) == 0 {
		m.selected = 0
		return
	}
	if m.selected >= len(m.services) {
		m.selected = len(m.services) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		// Handle mouse events only when ready
		if !m.ready {
			return m, nil
		}

		// Only handle left mouse button release (clicks)
		if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		// Check if any service zone was clicked
		for i := range m.services {
			zoneID := fmt.Sprintf("service_%d", i)
			if zone.Get(zoneID).InBounds(msg) {
				// Service was clicked, select it and quit
				if i < len(m.services) {
					m.selected = i
					m.ensureValidSelection()
					m.selectedService = m.services[m.selected]
					return m, tea.Quit
				}
			}
		}

		return m, nil

	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}

		switch msg.String() {
		case "?":
			m.showHelp = !m.showHelp
			m.updateContent()
			return m, nil
		case "esc", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.ensureValidSelection()
				m.updateContent()
			}
		case "down", "j":
			if m.selected < len(m.services)-1 {
				m.selected++
				m.ensureValidSelection()
				m.updateContent()
			}

		case "enter", " ":
			if len(m.services) > 0 {
				m.ensureValidSelection()
				// Return selected service
				m.selectedService = m.services[m.selected]
				return m, tea.Quit
			}

		case "e":
			// Edit pg_service.conf
			configPath := os.ExpandEnv("$HOME/.pg_service.conf")
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			cmd := exec.Command(editor, configPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				if err != nil {
					return fmt.Sprintf("Failed to edit config: %v", err)
				}
				// Reload services
				services, err := listServices()
				if err != nil {
					return fmt.Sprintf("Failed to reload services: %v", err)
				}
				m.services = services
				if m.selected >= len(services) && len(services) > 0 {
					m.selected = len(services) - 1
				}
				m.updateContent()
				return nil
			})
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height)
			m.viewport.Style = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))
			m.ready = true
			m.updateContent()
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height
		}

		m.updateContent()
	}

	return m, nil
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

	content += "Select a database service to monitor:\n"

	if m.err != "" {
		content += "Error: " + m.err
	} else if len(m.services) == 0 {
		content += "No services found in ~/.pg_service.conf\nPress 'e' to edit the configuration file."
	} else {
		for i, service := range m.services {
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

	helpText.WriteString(titleStyle.Render("Service Picker Help") + "\n\n")

	// Navigation
	helpText.WriteString(titleStyle.Render("Navigation:") + "\n")
	helpText.WriteString(keyStyle.Render("↑/k") + " " + descStyle.Render("move up") + "\n")
	helpText.WriteString(keyStyle.Render("↓/j") + " " + descStyle.Render("move down") + "\n")
	helpText.WriteString(keyStyle.Render("click") + " " + descStyle.Render("select service") + "\n")
	helpText.WriteString(keyStyle.Render("enter/space") + " " + descStyle.Render("select service") + "\n\n")

	// Configuration
	helpText.WriteString(titleStyle.Render("Configuration:") + "\n")
	helpText.WriteString(keyStyle.Render("e") + " " + descStyle.Render("edit ~/.pg_service.conf") + "\n\n")

	// System
	helpText.WriteString(titleStyle.Render("System:") + "\n")
	helpText.WriteString(keyStyle.Render("?") + " " + descStyle.Render("toggle help") + "\n")
	helpText.WriteString(keyStyle.Render("esc") + " " + descStyle.Render("quit") + "\n")

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
