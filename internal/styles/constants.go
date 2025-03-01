package styles

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	HighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3c71a8"))
	SuccessStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8cdb2f"))
	AppStyle       = lipgloss.NewStyle().
			Padding(1, 2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3c71a8"))

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3c71a8")).
			MarginBottom(1)

	ErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f25d94"))
	DocStyle   = lipgloss.NewStyle().Margin(1, 2)
	HelpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render
)
