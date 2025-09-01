package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
		},
	}
}

func (sp *ServicePicker) Run() (string, error) {
	p := tea.NewProgram(sp.model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return "", fmt.Errorf("failed to run service picker: %w", err)
	}
	return sp.model.selectedService, nil
}

func (m *PickerModel) Init() tea.Cmd {
	return nil
}

func (m *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.updateContent()
			}
		case "down", "j":
			if m.selected < len(m.services)-1 {
				m.selected++
				m.updateContent()
			}

		case "enter", " ":
			if len(m.services) > 0 {
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
	content += "\n\n"

	content += "Select a database service to monitor:\n"
	content += "Navigation: ↑/↓ or k/j to navigate, Enter to select, e to edit ~/.pg_service.conf, q to quit\n\n"

	if m.err != "" {
		content += "Error: " + m.err
	} else if len(m.services) == 0 {
		content += "No services found in ~/.pg_service.conf\nPress 'e' to edit the configuration file."
	} else {
		for i, service := range m.services {
			if i == m.selected {
				content += lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("86")).
					Render("▶ " + service)
			} else {
				content += "  " + service
			}
			content += "\n"
		}
	}

	m.viewport.SetContent(content)
}

func (m *PickerModel) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if !m.ready {
		return "Getting ready..."
	}

	return m.viewport.View()
}
