package model

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melkeydev/govm/internal/styles"
	"github.com/melkeydev/govm/internal/utils"
)

type Model struct {
	List              list.Model
	Versions          []utils.GoVersion
	Err               error
	Loading           bool
	Spinner           spinner.Model
	HomeDir           string
	GoVersionsDir     string
	CurrentTab        int
	DownloadProgress  float64
	InstallingVersion string
	Message           string
	MessageType       string // "success" or "error"
	InstalledTable    table.Model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		utils.FetchGoVersions,
		m.Spinner.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.CurrentTab = (m.CurrentTab + 1) % 2
			return m, nil
		case "i":
			if m.CurrentTab == 0 {
				selectedItem := m.List.SelectedItem().(styles.Item)
				for _, v := range m.Versions {
					if v.Version == selectedItem.Name && !v.Installed {
						m.Loading = true
						m.InstallingVersion = v.Version
						m.Message = ""
						return m, utils.DownloadAndInstall(v)
					}
				}
			}
		case "u":
			if m.CurrentTab == 0 {
				selectedItem := m.List.SelectedItem().(styles.Item)
				for _, v := range m.Versions {
					if v.Version == selectedItem.Name && v.Installed {
						m.Loading = true
						m.Message = fmt.Sprintf("Switching to Go %s...", v.Version)
						return m, utils.SwitchVersion(v)
					}
				}
				m.Message = "You need to install this version first. Press 'i' to install."
				m.MessageType = "error"
			}
		case "r":
			m.Loading = true
			m.Message = ""
			return m, utils.FetchGoVersions
		}
	case tea.WindowSizeMsg:
		h, v := styles.DocStyle.GetFrameSize()
		m.List.SetSize(msg.Width-h, msg.Height-v-6)
		m.InstalledTable.SetWidth(msg.Width - h)
		m.InstalledTable.SetHeight(msg.Height - v - 10)
		return m, nil
	case utils.ErrMsg:
		m.Err = msg
		m.Loading = false
		m.Message = msg.Error()
		m.MessageType = "error"
		return m, nil
	case utils.VersionsMsg:
		m.Versions = msg
		items := make([]list.Item, len(m.Versions))
		for i, v := range m.Versions {
			items[i] = styles.Item{
				Name:            v.Version,
				DescriptionText: "go" + v.Version + " " + v.Filename,
				Installed:       v.Installed,
				Active:          v.Active,
			}
		}
		m.List.SetItems(items)
		m.Loading = false
		m.updateInstalledTable()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd
	case utils.DownloadCompleteMsg:
		m.Loading = false
		m.InstallingVersion = ""
		for i, v := range m.Versions {
			if v.Version == msg.Version {
				m.Versions[i].Installed = true
				m.Versions[i].Path = msg.Path
				break
			}
		}
		items := m.List.Items()
		for i, it := range items {
			if it.(styles.Item).Name == msg.Version {
				updatedItem := it.(styles.Item)
				updatedItem.Installed = true
				items[i] = updatedItem
			}
		}
		m.List.SetItems(items)
		m.updateInstalledTable()
		m.Message = fmt.Sprintf("Successfully installed Go %s", msg.Version)
		m.MessageType = "success"
		return m, nil
	case utils.SwitchCompletedMsg:
		m.Loading = false

		// Update version statuses
		for i := range m.Versions {
			m.Versions[i].Active = (m.Versions[i].Version == msg.Version)
		}

		// Update list items
		items := m.List.Items()
		for i, it := range items {
			updatedItem := it.(styles.Item)
			updatedItem.Active = (updatedItem.Name == msg.Version)
			items[i] = updatedItem
		}
		m.List.SetItems(items)

		// Update table
		m.updateInstalledTable()

		// Clear success message with appropriate instructions
		if msg.ShimInPath {
			m.Message = fmt.Sprintf("Switched to Go %s! Run 'go version' to verify.", msg.Version)
		} else {
			m.Message = fmt.Sprintf("Switched to Go %s!\n\n%s",
				msg.Version, utils.GetShimPathInstructions())
		}

		m.MessageType = "success"
		return m, nil
	}

	// Handle list updates
	newListModel, cmd := m.List.Update(msg)
	m.List = newListModel
	cmds = append(cmds, cmd)

	// Handle installed table updates
	newTableModel, tableCmd := m.InstalledTable.Update(msg)
	m.InstalledTable = newTableModel
	cmds = append(cmds, tableCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) updateInstalledTable() {
	rows := []table.Row{}
	for _, v := range m.Versions {
		if v.Installed {
			status := ""
			if v.Active {
				status = "active"
			}
			rows = append(rows, table.Row{v.Version, v.Path, status})
		}
	}
	m.InstalledTable.SetRows(rows)
}

func (m Model) View() string {
	if m.Err != nil {
		return fmt.Sprintf("Error: %s\n\nPress any key to quit.", m.Err)
	}

	// Build components
	components := []string{}

	// 1. Header
	header := styles.TitleStyle.Render("GoVM - Go Version Manager")
	components = append(components, header)

	// 2. Warning banner if needed
	if !utils.IsShimInPath() {
		warningStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#FFCC00")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(1, 2).
			Align(lipgloss.Left)

		instructions := utils.GetShimPathInstructions()
		warningBanner := warningStyle.Render("⚠️  GoVM is not in your PATH  ⚠️\n\n" + instructions)
		components = append(components, warningBanner)
	}

	// 3. Tab navigation
	tabs := []string{"Available Versions", "Installed Versions"}
	tabContent := ""
	for i, tab := range tabs {
		if i == m.CurrentTab {
			tabContent += styles.HighlightStyle.Render("[ "+tab+" ]") + " "
		} else {
			tabContent += fmt.Sprintf("[ %s ]", tab) + " "
		}
	}
	components = append(components, tabContent)

	// 4. Tab-specific content
	if m.CurrentTab == 0 {
		// Available versions tab
		listView := m.List.View()
		components = append(components, listView)

		// Loading spinner
		if m.Loading {
			spinnerDisplay := ""
			if m.InstallingVersion != "" {
				progressBar := fmt.Sprintf("[downloading Go %s]", m.InstallingVersion)
				spinnerDisplay = fmt.Sprintf("%s %s", m.Spinner.View(), progressBar)
			} else {
				spinnerDisplay = fmt.Sprintf("%s Loading versions...", m.Spinner.View())
			}
			components = append(components, spinnerDisplay)
		}
	} else {
		// Installed versions tab
		tableView := m.InstalledTable.View()
		components = append(components, tableView)
	}

	// 5. Message display
	if m.Message != "" {
		if m.MessageType == "success" {
			components = append(components, styles.SuccessStyle.Render(m.Message))
		} else {
			components = append(components, styles.ErrorStyle.Render(m.Message))
		}
	}

	// 6. Help text
	if m.CurrentTab == 0 {
		components = append(components, styles.HelpStyle("\nPress 'i' to install, 'u' to use/switch, 'r' to refresh, 'tab' to switch tabs, 'q' to quit"))
	} else {
		components = append(components, styles.HelpStyle("\nPress 'tab' to switch tabs, 'q' to quit"))
	}

	// Join all components with newlines
	return styles.AppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, components...))
}
