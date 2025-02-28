package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type GoVersion struct {
	Version   string
	Filename  string
	URL       string
	Installed bool
	Active    bool
	Path      string
	Stable    bool
}

// TODO: move all styling into a sep package
var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3c71a8"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3c71a8")).
			MarginBottom(1)

	highlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3c71a8"))
	successStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8cdb2f"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f25d94"))
	docStyle       = lipgloss.NewStyle().Margin(1, 2)
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render
)

type item struct {
	title       string
	description string
	installed   bool
	active      bool
}

func (i item) Title() string {
	title := i.title
	if i.active {
		title = fmt.Sprintf("%s %s", title, successStyle.Render("(active)"))
	}
	if i.installed {
		title = fmt.Sprintf("%s %s", title, highlightStyle.Render("(installed)"))
	}
	return title
}

func (i item) Description() string { return i.description }
func (i item) FilterValue() string { return i.title }

type model struct {
	list              list.Model
	versions          []GoVersion
	err               error
	loading           bool
	spinner           spinner.Model
	homeDir           string
	goVersionsDir     string
	currentTab        int
	downloadProgress  float64
	installingVersion string
	message           string
	messageType       string // "success" or "error"
	installedTable    table.Model
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		fetchGoVersions,
		m.spinner.Tick,
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.currentTab = (m.currentTab + 1) % 2
			return m, nil
		case "i":
			if m.currentTab == 0 {
				selectedItem := m.list.SelectedItem().(item)
				for _, v := range m.versions {
					if v.Version == selectedItem.title && !v.Installed {
						m.loading = true
						m.installingVersion = v.Version
						m.message = ""
						return m, downloadAndInstall(v)
					}
				}
			}
		case "u":
			if m.currentTab == 0 {
				selectedItem := m.list.SelectedItem().(item)
				for _, v := range m.versions {
					if v.Version == selectedItem.title && v.Installed {
						m.loading = true
						m.message = fmt.Sprintf("Switching to Go %s...", v.Version)
						return m, switchVersion(v)
					}
				}
				m.message = "You need to install this version first. Press 'i' to install."
				m.messageType = "error"
			}
		case "r":
			m.loading = true
			m.message = ""
			return m, fetchGoVersions
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-6)

		m.installedTable.SetWidth(msg.Width - h)
		m.installedTable.SetHeight(msg.Height - v - 10)

		return m, nil

	case errMsg:
		m.err = msg
		m.loading = false
		m.message = msg.Error()
		m.messageType = "error"
		return m, nil

	case versionsMsg:
		m.versions = msg

		items := make([]list.Item, len(m.versions))
		for i, v := range m.versions {
			items[i] = item{
				title:       v.Version,
				description: "go" + v.Version + " " + v.Filename,
				installed:   v.Installed,
				active:      v.Active,
			}
		}

		m.list.SetItems(items)
		m.loading = false

		m.updateInstalledTable()

		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case downloadCompleteMsg:
		m.loading = false
		m.installingVersion = ""

		for i, v := range m.versions {
			if v.Version == msg.version {
				m.versions[i].Installed = true
				m.versions[i].Path = msg.path
				break
			}
		}

		items := m.list.Items()
		for i, it := range items {
			if it.(item).title == msg.version {
				updatedItem := it.(item)
				updatedItem.installed = true
				items[i] = updatedItem
			}
		}
		m.list.SetItems(items)

		m.updateInstalledTable()

		m.message = fmt.Sprintf("Successfully installed Go %s", msg.version)
		m.messageType = "success"
		return m, nil

	case switchCompletedMsg:
		m.loading = false

		for i := range m.versions {
			m.versions[i].Active = (m.versions[i].Version == msg.version)
		}

		items := m.list.Items()
		for i, it := range items {
			updatedItem := it.(item)
			updatedItem.active = (updatedItem.title == msg.version)
			items[i] = updatedItem
		}
		m.list.SetItems(items)

		m.updateInstalledTable()

		m.message = fmt.Sprintf("Switched to Go %s", msg.version)
		m.messageType = "success"
		return m, nil
	}

	newListModel, cmd := m.list.Update(msg)
	m.list = newListModel
	cmds = append(cmds, cmd)

	newTableModel, tableCmd := m.installedTable.Update(msg)
	m.installedTable = newTableModel
	cmds = append(cmds, tableCmd)

	return m, tea.Batch(cmds...)
}

func (m *model) updateInstalledTable() {
	rows := []table.Row{}

	for _, v := range m.versions {
		if v.Installed {
			status := ""
			if v.Active {
				status = "active"
			}
			rows = append(rows, table.Row{v.Version, v.Path, status})
		}
	}

	m.installedTable.SetRows(rows)
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %s\n\nPress any key to quit.", m.err)
	}

	var content string

	header := titleStyle.Render("GoVM - Go Version Manager")

	tabs := []string{"Available Versions", "Installed Versions"}
	tabContent := ""
	for i, tab := range tabs {
		if i == m.currentTab {
			tabContent += highlightStyle.Render("[ "+tab+" ]") + " "
		} else {
			tabContent += fmt.Sprintf("[ %s ]", tab) + " "
		}
	}

	messageDisplay := ""
	if m.message != "" {
		if m.messageType == "success" {
			messageDisplay = successStyle.Render(m.message)
		} else {
			messageDisplay = errorStyle.Render(m.message)
		}
	}

	if m.currentTab == 0 {
		listView := m.list.View()

		help := helpStyle("\nPress 'i' to install, 'u' to use/switch, 'r' to refresh, 'tab' to switch tabs, 'q' to quit")

		spinnerDisplay := ""
		if m.loading {
			if m.installingVersion != "" {
				progressBar := fmt.Sprintf("[downloading Go %s]", m.installingVersion)
				spinnerDisplay = fmt.Sprintf("%s %s", m.spinner.View(), progressBar)
			} else {
				spinnerDisplay = fmt.Sprintf("%s Loading versions...", m.spinner.View())
			}
		}

		content = fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s\n%s\n%s",
			header,
			tabContent,
			listView,
			spinnerDisplay,
			messageDisplay,
			help)
	} else {
		tableView := m.installedTable.View()
		help := helpStyle("\nPress 'tab' to switch tabs, 'q' to quit")

		content = fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s\n%s",
			header,
			tabContent,
			tableView,
			messageDisplay,
			help)
	}

	return appStyle.Render(content)
}

type errMsg error

type versionsMsg []GoVersion

type downloadCompleteMsg struct {
	version string
	path    string
}

type switchCompletedMsg struct {
	version string
}

func fetchGoVersions() tea.Msg {
	resp, err := http.Get("https://go.dev/dl/?mode=json&include=all")
	if err != nil {
		return errMsg(fmt.Errorf("failed to connect to go.dev: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errMsg(err)
	}

	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Files   []struct {
			Filename string `json:"filename"`
			OS       string `json:"os"`
			Arch     string `json:"arch"`
			Size     int    `json:"size"`
		} `json:"files"`
	}

	err = json.Unmarshal(body, &releases)
	if err != nil {
		return errMsg(fmt.Errorf("failed to parse API response: %v", err))
	}

	currentOS := runtime.GOOS
	arch := runtime.GOARCH

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errMsg(err)
	}

	goVersionsDir := filepath.Join(homeDir, ".govm", "versions")
	err = os.MkdirAll(goVersionsDir, 0755)
	if err != nil {
		return errMsg(err)
	}

	activeVersion := getCurrentGoVersion()

	installedVersions := map[string]string{}
	entries, _ := os.ReadDir(goVersionsDir)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
			versionPath := filepath.Join(goVersionsDir, entry.Name())
			version := strings.TrimPrefix(entry.Name(), "go")

			goBin := filepath.Join(versionPath, "bin", "go")
			if _, err := os.Stat(goBin); err == nil {
				installedVersions[version] = versionPath
			}
		}
	}

	var versions []GoVersion
	for _, release := range releases {
		version := strings.TrimPrefix(release.Version, "go")

		for _, file := range release.Files {
			if file.OS == currentOS && file.Arch == arch {
				v := GoVersion{
					Version:   version,
					Filename:  file.Filename,
					URL:       "https://go.dev/dl/" + file.Filename,
					Installed: false,
					Active:    false,
					Stable:    release.Stable,
				}

				if path, ok := installedVersions[version]; ok {
					v.Installed = true
					v.Path = path
				}

				if activeVersion == version {
					v.Active = true
				}

				versions = append(versions, v)
				break
			}
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		iParts := strings.Split(versions[i].Version, ".")
		jParts := strings.Split(versions[j].Version, ".")

		if len(iParts) > 0 && len(jParts) > 0 {
			iMajor, _ := strconv.Atoi(iParts[0])
			jMajor, _ := strconv.Atoi(jParts[0])
			if iMajor != jMajor {
				return iMajor > jMajor
			}
		}

		if len(iParts) > 1 && len(jParts) > 1 {
			iMinor, _ := strconv.Atoi(iParts[1])
			jMinor, _ := strconv.Atoi(jParts[1])
			if iMinor != jMinor {
				return iMinor > jMinor
			}
		}

		if len(iParts) > 2 && len(jParts) > 2 {
			iPatch, _ := strconv.Atoi(iParts[2])
			jPatch, _ := strconv.Atoi(jParts[2])
			return iPatch > jPatch
		}

		return versions[i].Version > versions[j].Version
	})

	return versionsMsg(versions)
}

func downloadAndInstall(version GoVersion) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errMsg(err)
		}

		goVersionsDir := filepath.Join(homeDir, ".govm", "versions")
		downloadDir := filepath.Join(homeDir, ".govm", "downloads")

		for _, dir := range []string{goVersionsDir, downloadDir} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return errMsg(err)
			}
		}

		versionDir := filepath.Join(goVersionsDir, fmt.Sprintf("go%s", version.Version))
		if _, err := os.Stat(versionDir); err == nil {
			if err := os.RemoveAll(versionDir); err != nil {
				return errMsg(fmt.Errorf("failed to remove existing installation: %v", err))
			}
		}

		downloadPath := filepath.Join(downloadDir, version.Filename)

		if _, err := os.Stat(downloadPath); err == nil {
			if err := os.Remove(downloadPath); err != nil {
				return errMsg(fmt.Errorf("failed to remove existing download: %v", err))
			}
		}

		resp, err := http.Get(version.URL)
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		out, err := os.Create(downloadPath)
		if err != nil {
			return errMsg(err)
		}
		defer out.Close()

		written, err := io.Copy(out, resp.Body)
		if err != nil {
			return errMsg(err)
		}

		if written == 0 {
			return errMsg(fmt.Errorf("downloaded empty file"))
		}

		out.Close()

		entries, _ := os.ReadDir(goVersionsDir)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
				dirPath := filepath.Join(goVersionsDir, entry.Name())
				if dirPath != versionDir {
					os.RemoveAll(dirPath)
				}
			}
		}

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			if strings.HasSuffix(version.Filename, ".zip") {
				cmd = exec.Command("powershell", "-Command",
					fmt.Sprintf("Expand-Archive -Path \"%s\" -DestinationPath \"%s\" -Force",
						downloadPath, goVersionsDir))
			} else {
				return errMsg(fmt.Errorf("unsupported archive format for Windows: %s", version.Filename))
			}
		} else {
			if strings.HasSuffix(version.Filename, ".tar.gz") {
				cmd = exec.Command("tar", "-xzf", downloadPath, "-C", goVersionsDir)
			} else {
				return errMsg(fmt.Errorf("unsupported archive format for Unix: %s", version.Filename))
			}
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return errMsg(fmt.Errorf("extraction error: %v\nOutput: %s", err, string(output)))
		}

		if runtime.GOOS != "windows" {
			goBin := filepath.Join(versionDir, "bin", "go")
			if _, err := os.Stat(goBin); err == nil {
				os.Chmod(goBin, 0755)
			}
		}

		goBin := filepath.Join(versionDir, "bin", "go")
		if _, err := os.Stat(goBin); os.IsNotExist(err) {
			// The extract may have used a different directory name
			entries, _ := os.ReadDir(goVersionsDir)
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
					testPath := filepath.Join(goVersionsDir, entry.Name(), "bin", "go")
					if _, err := os.Stat(testPath); err == nil {
						sourcePath := filepath.Join(goVersionsDir, entry.Name())
						if sourcePath != versionDir {
							if err := os.Rename(sourcePath, versionDir); err != nil {
								return errMsg(fmt.Errorf("failed to rename directory: %v", err))
							}
						}
						break
					}
				}
			}
		}

		if _, err := os.Stat(goBin); os.IsNotExist(err) {
			return errMsg(fmt.Errorf("installation failed: Go binary not found at %s", goBin))
		}

		verifyCmd := exec.Command(goBin, "version")
		verifyOutput, err := verifyCmd.CombinedOutput()
		if err != nil {
			return errMsg(fmt.Errorf("Go binary verification failed: %v\nOutput: %s", err, string(verifyOutput)))
		}

		return downloadCompleteMsg{version: version.Version, path: versionDir}
	}
}

func switchVersion(version GoVersion) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errMsg(err)
		}

		govmDir := filepath.Join(homeDir, ".govm")
		if err := os.MkdirAll(govmDir, 0755); err != nil {
			return errMsg(err)
		}

		if err := createShellConfigs(govmDir, version.Path, version.Version); err != nil {
			return errMsg(err)
		}

		if runtime.GOOS != "windows" {
			symlink := filepath.Join(govmDir, "current")
			os.Remove(symlink) // Ignore error if it doesn't exist
			if err := os.Symlink(version.Path, symlink); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create symlink: %v\n", err)
			}
		}

		return switchCompletedMsg{version: version.Version}
	}
}

func createShellConfigs(govmDir, versionDir, version string) error {
	shellConfig := filepath.Join(govmDir, "govm.sh")
	shellContent := fmt.Sprintf(`#!/bin/bash
# GoVM configuration - Go %s
export GOROOT="%s"
export PATH="$GOROOT/bin:$PATH"
echo "Activated Go %s"
`, version, versionDir, version)

	if err := os.WriteFile(shellConfig, []byte(shellContent), 0755); err != nil {
		return fmt.Errorf("failed to create shell config: %v", err)
	}

	// For Windows
	// TBH I have no clue if this works or not
	if runtime.GOOS == "windows" {
		batchFile := filepath.Join(govmDir, "govm.bat")
		batchContent := fmt.Sprintf(`@echo off
REM GoVM configuration - Go %s
SET GOROOT=%s
SET PATH=%%GOROOT%%\bin;%%PATH%%
echo Activated Go %s
`, version, versionDir, version)

		if err := os.WriteFile(batchFile, []byte(batchContent), 0755); err != nil {
			return fmt.Errorf("failed to create batch file: %v", err)
		}

		// PowerShell script
		psFile := filepath.Join(govmDir, "govm.ps1")
		psContent := fmt.Sprintf(`# GoVM configuration - Go %s
$env:GOROOT = "%s"
$env:PATH = "$env:GOROOT\bin;$env:PATH"
Write-Host "Activated Go %s"
`, version, versionDir, version)

		if err := os.WriteFile(psFile, []byte(psContent), 0755); err != nil {
			return fmt.Errorf("failed to create PowerShell script: %v", err)
		}
	}

	return nil
}

func getCurrentGoVersion() string {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	parts := strings.Split(string(output), " ")
	if len(parts) >= 3 {
		return strings.TrimPrefix(parts[2], "go")
	}

	return ""
}

func main() {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#3c71a8"))

	columns := []table.Column{
		{Title: "Version", Width: 10},
		{Title: "Path", Width: 40},
		{Title: "Status", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	t.SetStyles(table.Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#3c71a8")).
			Padding(0, 1),
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#3c71a8")).
			Bold(true).
			Padding(0, 1),
		Cell: lipgloss.NewStyle().
			Padding(0, 1),
	})

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		os.Exit(1)
	}

	goVersionsDir := filepath.Join(homeDir, ".govm", "versions")

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#3c71a8")).
		Bold(true)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#DDDDDD")).
		Background(lipgloss.Color("#3c71a8"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Go Versions"
	l.SetShowHelp(false)

	initialModel := model{
		list:           l,
		versions:       []GoVersion{},
		spinner:        s,
		loading:        true,
		homeDir:        homeDir,
		goVersionsDir:  goVersionsDir,
		installedTable: t,
	}

	p := tea.NewProgram(initialModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
