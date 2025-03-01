package utils

import (
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

type DownloadCompleteMsg struct {
	Version string
	Path    string
}

type SwitchCompletedMsg struct {
	Version string
}

func FetchGoVersions() tea.Msg {
	resp, err := http.Get("https://go.dev/dl/?mode=json&include=all")
	if err != nil {
		return ErrMsg(fmt.Errorf("failed to connect to go.dev: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrMsg(err)
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
		return ErrMsg(fmt.Errorf("failed to parse API response: %v", err))
	}

	currentOS := runtime.GOOS
	arch := runtime.GOARCH

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ErrMsg(err)
	}

	goVersionsDir := filepath.Join(homeDir, ".govm", "versions")
	err = os.MkdirAll(goVersionsDir, 0755)
	if err != nil {
		return ErrMsg(err)
	}

	activeVersion := GetCurrentGoVersion()

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

	return VersionsMsg(versions)
}

func GetCurrentGoVersion() string {
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

func DownloadAndInstall(version GoVersion) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ErrMsg(err)
		}

		goVersionsDir := filepath.Join(homeDir, ".govm", "versions")
		downloadDir := filepath.Join(homeDir, ".govm", "downloads")

		for _, dir := range []string{goVersionsDir, downloadDir} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return ErrMsg(err)
			}
		}

		versionDir := filepath.Join(goVersionsDir, fmt.Sprintf("go%s", version.Version))
		if _, err := os.Stat(versionDir); err == nil {
			if err := os.RemoveAll(versionDir); err != nil {
				return ErrMsg(fmt.Errorf("failed to remove existing installation: %v", err))
			}
		}

		downloadPath := filepath.Join(downloadDir, version.Filename)

		if _, err := os.Stat(downloadPath); err == nil {
			if err := os.Remove(downloadPath); err != nil {
				return ErrMsg(fmt.Errorf("failed to remove existing download: %v", err))
			}
		}

		resp, err := http.Get(version.URL)
		if err != nil {
			return ErrMsg(err)
		}
		defer resp.Body.Close()

		out, err := os.Create(downloadPath)
		if err != nil {
			return ErrMsg(err)
		}
		defer out.Close()

		written, err := io.Copy(out, resp.Body)
		if err != nil {
			return ErrMsg(err)
		}

		if written == 0 {
			return ErrMsg(fmt.Errorf("downloaded empty file"))
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
				return ErrMsg(fmt.Errorf("unsupported archive format for Windows: %s", version.Filename))
			}
		} else {
			if strings.HasSuffix(version.Filename, ".tar.gz") {
				cmd = exec.Command("tar", "-xzf", downloadPath, "-C", goVersionsDir)
			} else {
				return ErrMsg(fmt.Errorf("unsupported archive format for Unix: %s", version.Filename))
			}
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return ErrMsg(fmt.Errorf("extraction error: %v\nOutput: %s", err, string(output)))
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
								return ErrMsg(fmt.Errorf("failed to rename directory: %v", err))
							}
						}
						break
					}
				}
			}
		}

		if _, err := os.Stat(goBin); os.IsNotExist(err) {
			return ErrMsg(fmt.Errorf("installation failed: Go binary not found at %s", goBin))
		}

		verifyCmd := exec.Command(goBin, "version")
		verifyOutput, err := verifyCmd.CombinedOutput()
		if err != nil {
			return ErrMsg(fmt.Errorf("Go binary verification failed: %v\nOutput: %s", err, string(verifyOutput)))
		}

		return DownloadCompleteMsg{Version: version.Version, Path: versionDir}
	}
}

func SwitchVersion(version GoVersion) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ErrMsg(err)
		}

		govmDir := filepath.Join(homeDir, ".govm")
		if err := os.MkdirAll(govmDir, 0755); err != nil {
			return ErrMsg(err)
		}

		if err := createShellConfigs(govmDir, version.Path, version.Version); err != nil {
			return ErrMsg(err)
		}

		if runtime.GOOS != "windows" {
			symlink := filepath.Join(govmDir, "current")
			os.Remove(symlink) // Ignore error if it doesn't exist
			if err := os.Symlink(version.Path, symlink); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create symlink: %v\n", err)
			}
		}

		return SwitchCompletedMsg{Version: version.Version}
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
