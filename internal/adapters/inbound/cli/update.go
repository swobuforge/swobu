package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

const unixInstallerURL = "https://swobu.com/install.sh"
const windowsInstallerURL = "https://swobu.com/install.ps1"

func runUpdate(ctx context.Context, stdout io.Writer, stderr io.Writer, args []string, runner Runner) ExitCode {
	if len(args) != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: swobu update")
		return ExitDown
	}

	currentVersion := normalizeSemver(currentSwobuVersion())
	if !semver.IsValid(currentVersion) {
		_, _ = fmt.Fprintln(stderr, "error: this Swobu build is not a released standalone installation")
		_, _ = fmt.Fprintln(stderr, "update it from its source checkout")
		return ExitDown
	}

	executable := runner.UpdateExecutable
	if executable == nil {
		executable = os.Executable
	}
	actualPath, err := executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: determine current executable: %v\n", err)
		return ExitDown
	}
	if runner.UpdateExecutable == nil {
		info, err := os.Lstat(actualPath)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "error: inspect current executable: %v\n", err)
			return ExitDown
		}
		if !info.Mode().IsRegular() {
			_, _ = fmt.Fprintln(stderr, "error: this Swobu installation is not managed by `swobu update`")
			_, _ = fmt.Fprintln(stderr, "the current executable must be a regular file at the standalone path")
			return ExitDown
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: locate home directory: %v\n", err)
		return ExitDown
	}
	expectedPath, installerURL, err := managedInstallTarget(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return ExitDown
	}
	if !sameInstallPath(runtime.GOOS, actualPath, expectedPath) {
		_, _ = fmt.Fprintln(stderr, "error: this Swobu installation is not managed by `swobu update`")
		_, _ = fmt.Fprintln(stderr, "update it using the method that installed it")
		return ExitDown
	}

	runInstaller := runner.RunInstaller
	if runInstaller == nil {
		runInstaller = defaultRunInstaller
	}
	if err := runInstaller(ctx, &http.Client{}, installerURL, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: update failed: %v\n", err)
		return ExitDown
	}
	return ExitHealthy
}

func managedInstallTarget(goos string, home string, localAppData string) (string, string, error) {
	switch goos {
	case "windows":
		if strings.TrimSpace(localAppData) == "" {
			return "", "", fmt.Errorf("LOCALAPPDATA is required to locate the managed installation")
		}
		return filepath.Join(localAppData, "Programs", "swobu", "bin", "swobu.exe"), windowsInstallerURL, nil
	case "darwin", "linux":
		return filepath.Join(home, ".local", "bin", "swobu"), unixInstallerURL, nil
	default:
		return "", "", fmt.Errorf("unsupported update platform: %s", goos)
	}
}

func sameInstallPath(goos string, actual string, expected string) bool {
	actual = filepath.Clean(actual)
	expected = filepath.Clean(expected)
	if goos == "windows" {
		return strings.EqualFold(actual, expected)
	}
	return actual == expected
}

func defaultRunInstaller(ctx context.Context, client *http.Client, installerURL string, stdout io.Writer, stderr io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, installerURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("installer download returned status %d", response.StatusCode)
	}

	suffix := ".sh"
	if runtime.GOOS == "windows" {
		suffix = ".ps1"
	}
	installer, err := os.CreateTemp("", "swobu-installer-*"+suffix)
	if err != nil {
		return err
	}
	installerPath := installer.Name()
	defer func() { _ = os.Remove(installerPath) }()
	if _, err := io.Copy(installer, response.Body); err != nil {
		_ = installer.Close()
		return err
	}
	if err := installer.Close(); err != nil {
		return err
	}

	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", installerPath, "-NoStart")
	} else {
		command = exec.CommandContext(ctx, "sh", installerPath, "--no-start")
	}
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = append(os.Environ(), "START_SWOBU=false")
	return command.Run()
}

func normalizeSemver(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}
