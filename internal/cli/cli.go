package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/melkeydev/govm/internal/utils"
)

func InstallVersion(version string) {
	fmt.Printf("🔍 Looking for Go version matching %s...\n", version)

	matchedVersion, err := findMatchingVersion(version)
	if err != nil {
		fmt.Printf("❌ %s\n", err)
		return
	}

	fmt.Printf("📥 Installing Go %s...\n", matchedVersion.Version)

	done := make(chan bool)
	errCh := make(chan error)

	go func() {
		msg := utils.DownloadAndInstall(matchedVersion)()

		switch msg := msg.(type) {
		case utils.ErrMsg:
			errCh <- msg
		case utils.DownloadCompleteMsg:
			done <- true
		}
	}()

	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinIdx := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Printf("\r✅ Successfully installed Go %s\n", matchedVersion.Version)
			fmt.Printf("👉 To activate this version, run: govm use %s\n", matchedVersion.Version)
			return
		case err := <-errCh:
			fmt.Printf("\r❌ Installation failed: %v\n", err)
			return
		case <-ticker.C:
			fmt.Printf("\r%s Installing Go %s...", spinChars[spinIdx], matchedVersion.Version)
			spinIdx = (spinIdx + 1) % len(spinChars)
		}
	}
}

func UseVersion(version string) {
	fmt.Printf("🔍 Looking for installed Go version matching %s...\n", version)

	matchedVersion, err := findInstalledVersion(version)
	if err != nil {
		fmt.Printf("❌ %s\n", err)
		return
	}

	fmt.Printf("🔄 Switching to Go %s...\n", matchedVersion.Version)

	msg := utils.SwitchVersion(matchedVersion)()

	switch msg := msg.(type) {
	case utils.ErrMsg:
		fmt.Printf("❌ Failed to switch version: %v\n", msg)
	case utils.SwitchCompletedMsg:
		fmt.Printf("✅ Switched to Go %s\n", matchedVersion.Version)

		if !utils.IsShimInPath() {
			fmt.Println("\n⚠️  GoVM is not in your PATH")
			fmt.Println(utils.GetShimPathInstructions())
		} else {
			fmt.Println("🚀 Run 'go version' in a new terminal to verify")
		}
	}
}

func ListVersions() {
	fmt.Println("📋 Installed Go Versions:")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("❌ Error getting home directory: %v\n", err)
		return
	}

	activeVersion := ""
	activeVersionFile := filepath.Join(homeDir, ".govm", "active_version")
	if versionBytes, err := os.ReadFile(activeVersionFile); err == nil {
		activeVersion = string(versionBytes)
	}

	goVersionsDir := filepath.Join(homeDir, ".govm", "versions")

	if _, err := os.Stat(goVersionsDir); os.IsNotExist(err) {
		fmt.Println("  No versions installed yet")
		return
	}

	entries, err := os.ReadDir(goVersionsDir)
	if err != nil {
		fmt.Printf("❌ Error reading versions directory: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("  No versions installed yet")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
			version := strings.TrimPrefix(entry.Name(), "go")

			if version == activeVersion {
				fmt.Printf("  %s %s\n", version, "✓ (active)")
			} else {
				fmt.Printf("  %s\n", version)
			}
		}
	}

	fmt.Println("\nTo install a new version: govm install <version>")
	fmt.Println("To switch versions: govm use <version>")
}

func findMatchingVersion(version string) (utils.GoVersion, error) {
	msg := utils.FetchGoVersions()

	versions, ok := msg.(utils.VersionsMsg)
	if !ok {
		if errMsg, isErr := msg.(utils.ErrMsg); isErr {
			return utils.GoVersion{}, fmt.Errorf("failed to fetch versions: %v", errMsg)
		}
		return utils.GoVersion{}, fmt.Errorf("failed to fetch versions")
	}

	for _, v := range versions {
		if v.Version == version {
			return v, nil
		}
	}

	prefix := version + "."
	var matchedVersion utils.GoVersion
	found := false

	for _, v := range versions {
		if strings.HasPrefix(v.Version, prefix) {
			if !found || compareVersions(v.Version, matchedVersion.Version) > 0 {
				matchedVersion = v
				found = true
			}
		}
	}

	if !found && !strings.Contains(version, ".") {
		prefix = version + "."
		for _, v := range versions {
			if strings.HasPrefix(v.Version, prefix) {
				if !found || compareVersions(v.Version, matchedVersion.Version) > 0 {
					matchedVersion = v
					found = true
				}
			}
		}
	}

	if found {
		return matchedVersion, nil
	}

	return utils.GoVersion{}, fmt.Errorf("no version matching '%s' found", version)
}

func findInstalledVersion(version string) (utils.GoVersion, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return utils.GoVersion{}, fmt.Errorf("failed to get home directory: %v", err)
	}

	goVersionsDir := filepath.Join(homeDir, ".govm", "versions")

	versionDir := filepath.Join(goVersionsDir, "go"+version)
	if _, err := os.Stat(versionDir); err == nil {
		return utils.GoVersion{
			Version:   version,
			Path:      versionDir,
			Installed: true,
		}, nil
	}

	entries, err := os.ReadDir(goVersionsDir)
	if err != nil {
		return utils.GoVersion{}, fmt.Errorf("failed to read versions directory: %v", err)
	}

	prefix := "go" + version + "."
	var matchedVersion utils.GoVersion
	found := false

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			versionPath := filepath.Join(goVersionsDir, entry.Name())
			versionStr := strings.TrimPrefix(entry.Name(), "go")

			if !found || compareVersions(versionStr, matchedVersion.Version) > 0 {
				matchedVersion = utils.GoVersion{
					Version:   versionStr,
					Path:      versionPath,
					Installed: true,
				}
				found = true
			}
		}
	}

	if !found && !strings.Contains(version, ".") {
		prefix = "go" + version + "."
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
				versionPath := filepath.Join(goVersionsDir, entry.Name())
				versionStr := strings.TrimPrefix(entry.Name(), "go")

				if !found || compareVersions(versionStr, matchedVersion.Version) > 0 {
					matchedVersion = utils.GoVersion{
						Version:   versionStr,
						Path:      versionPath,
						Installed: true,
					}
					found = true
				}
			}
		}
	}

	if found {
		return matchedVersion, nil
	}

	return utils.GoVersion{}, fmt.Errorf("no installed version matching '%s' found", version)
}

func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		p1, _ := strconv.Atoi(parts1[i])
		p2, _ := strconv.Atoi(parts2[i])

		if p1 < p2 {
			return -1
		}
		if p1 > p2 {
			return 1
		}
	}

	if len(parts1) < len(parts2) {
		return -1
	}
	if len(parts1) > len(parts2) {
		return 1
	}

	return 0
}
