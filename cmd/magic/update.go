package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// VersionInfo 版本信息
type VersionInfo struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

var (
	updateCheckFlag   bool
	updateChannelFlag string
	updateBackupFlag  bool
	updateForceFlag   bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update to the latest version",
	Long:  `Check for updates and install the latest version of magic.`,
	Run:   runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckFlag, "check", false, "Check for updates without installing")
	updateCmd.Flags().BoolVar(&updateBackupFlag, "backup", true, "Create backup before updating")
	updateCmd.Flags().BoolVar(&updateForceFlag, "force", false, "Force update even if same version")
	updateCmd.Flags().StringVar(&updateChannelFlag, "channel", "stable", "Update channel: stable, beta, nightly")

	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║               Magic Agent Updater                       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 获取当前版本
	currentVersion := getCurrentVersion()
	fmt.Printf("  Current version: %s\n", currentVersion)
	fmt.Printf("  Update channel:  %s\n", updateChannelFlag)
	fmt.Println()

	// 检查更新
	latest, err := checkForUpdates(updateChannelFlag)
	if err != nil {
		fmt.Printf("  ⚠ Failed to check for updates: %v\n", err)
		fmt.Println("  Continuing with local update...")
	}

	if latest != nil {
		fmt.Printf("  Latest version:  %s\n", latest.Tag)
		if latest.Prerelease {
			fmt.Println("  ⚠ This is a pre-release version")
		}
		fmt.Println()

		if !updateForceFlag && latest.Tag == currentVersion {
			fmt.Println("  ✓ Already on the latest version!")
			return
		}

		if updateCheckFlag {
			fmt.Println("  Run 'magic update' to install the update.")
			return
		}

		// 确认更新
		fmt.Print("  Do you want to update? (Y/n): ")
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		confirm = strings.TrimSpace(strings.ToLower(confirm))

		if confirm == "n" {
			fmt.Println("\n  Update cancelled.")
			return
		}
	}

	// 执行更新
	if err := performUpdate(currentVersion, latest); err != nil {
		fmt.Printf("\n  ✗ Update failed: %v\n", err)
		if updateBackupFlag {
			fmt.Println("\n  Attempting rollback...")
			if err := rollback(); err != nil {
				fmt.Printf("  ✗ Rollback failed: %v\n", err)
			}
		}
		os.Exit(1)
	}

	fmt.Println("\n  ✓ Update complete!")
	fmt.Printf("  Please restart magic to use the new version.\n")
}

func getCurrentVersion() string {
	execPath, err := os.Executable()
	if err != nil {
		return "unknown"
	}

	// 检查是否为 git 仓库
	dir := filepath.Dir(execPath)
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		gitCmd := exec.Command("git", "-C", dir, "describe", "--tags", "--abbrev=0")
		if output, err := gitCmd.Output(); err == nil {
			return strings.TrimSpace(string(output))
		}
	}

	return "dev"
}

func checkForUpdates(channel string) (*VersionInfo, error) {
	owner := "magicwubiao"
	repo := "go-magic"

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "go-magic/update")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var release VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// findAsset finds the matching asset for the current platform from the release assets list
func findAsset(release *VersionInfo) *Asset {
	if release == nil {
		return nil
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Build expected name patterns to match against
	// Actual release names: go-magic-windows-amd64.exe, go-magic-linux-amd64, etc.
	var patterns []string
	if goos == "windows" {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s.exe", goos, goarch),
			fmt.Sprintf("magic-%s-%s.exe", goos, goarch),
		}
	} else {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s", goos, goarch),
			fmt.Sprintf("magic-%s-%s", goos, goarch),
		}
	}

	for _, asset := range release.Assets {
		for _, pattern := range patterns {
			if asset.Name == pattern {
				return &asset
			}
		}
	}

	// Fallback: try substring match
	expectedSub := fmt.Sprintf("%s-%s", goos, goarch)
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, expectedSub) {
			return &asset
		}
	}

	return nil
}

func performUpdate(currentVersion string, latest *VersionInfo) error {
	// 创建备份
	if updateBackupFlag {
		fmt.Println("  📦 Creating backup...")
		if err := createBackup(); err != nil {
			fmt.Printf("  ⚠ Backup failed: %v\n", err)
		} else {
			fmt.Println("  ✓ Backup created")
		}
	}

	// 如果没有获取到最新版本信息，使用 go install
	if latest == nil {
		fmt.Println("  Using go install...")
		return performGoInstall()
	}

	// 从 assets 中查找匹配当前平台的文件
	asset := findAsset(latest)
	if asset == nil {
		fmt.Println("  ⚠ No matching binary found in release assets")
		fmt.Println("  Falling back to go install...")
		return performGoInstall()
	}

	// 下载
	fmt.Println()
	fmt.Printf("  📥 Downloading %s (%s)...\n", asset.Name, formatBytes(uint64(asset.Size)))
	tmpDir := filepath.Join(os.TempDir(), "magic-update")
	os.MkdirAll(tmpDir, 0755)
	downloadPath := filepath.Join(tmpDir, asset.Name)

	if err := downloadFile(asset.BrowserDownloadURL, downloadPath); err != nil {
		fmt.Printf("  ⚠ Download failed: %v\n", err)
		fmt.Println("  Falling back to go install...")
		return performGoInstall()
	}

	// 安装（直接替换，因为 release 中的文件是裸二进制）
	fmt.Println("  ⚙ Installing...")
	execPath, _ := os.Executable()

	if err := copyFile(downloadPath, execPath); err != nil {
		return fmt.Errorf("failed to install: %w", err)
	}

	// 清理
	os.RemoveAll(tmpDir)

	return nil
}

func performGoInstall() error {
	fmt.Println("  Using go install...")

	installCmd := exec.Command("go", "install", "github.com/magicwubiao/go-magic/cmd/magic@latest")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	return installCmd.Run()
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic/update")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	size := resp.ContentLength
	downloaded := int64(0)
	buf := make([]byte, 32*1024)

	reader := bufio.NewReader(resp.Body)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			downloaded += int64(n)

			if size > 0 {
				percent := float64(downloaded) / float64(size) * 100
				fmt.Printf("\r  Progress: %.1f%% (%s / %s)", percent, formatBytes(uint64(downloaded)), formatBytes(uint64(size)))
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	if size > 0 {
		fmt.Print("\r  Progress: 100.0%")
	} else {
		fmt.Printf("  Downloaded: %s", formatBytes(uint64(downloaded)))
	}
	fmt.Println()
	return nil
}

func createBackup() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	homeDir, _ := os.UserHomeDir()
	backupDir := filepath.Join(homeDir, ".magic", "backups")
	os.MkdirAll(backupDir, 0755)

	backupName := fmt.Sprintf("magic-backup-%s", getCurrentVersion())
	backupPath := filepath.Join(backupDir, backupName)

	return copyFile(execPath, backupPath)
}

func rollback() error {
	homeDir, _ := os.UserHomeDir()
	backupDir := filepath.Join(homeDir, ".magic", "backups")

	files, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}

	var latest string
	var latestTime int64
	for _, f := range files {
		if info, err := f.Info(); err == nil {
			if info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				latest = filepath.Join(backupDir, f.Name())
			}
		}
	}

	if latest == "" {
		return fmt.Errorf("no backup found")
	}

	execPath, _ := os.Executable()
	return copyFile(latest, execPath)
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
