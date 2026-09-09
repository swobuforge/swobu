package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpdateRefusesDevelopmentBuildBeforeInstaller(t *testing.T) {
	originalCurrent := currentSwobuVersion
	currentSwobuVersion = func() string { return "dev" }
	t.Cleanup(func() { currentSwobuVersion = originalCurrent })

	installerCalled := false
	var stderr bytes.Buffer
	exitCode := runUpdate(context.Background(), &bytes.Buffer{}, &stderr, nil, Runner{
		UpdateExecutable: func() (string, error) {
			t.Fatal("executable path checked for development build")
			return "", nil
		},
		RunInstaller: func(context.Context, *http.Client, string, io.Writer, io.Writer) error {
			installerCalled = true
			return nil
		},
	})
	if exitCode != ExitDown || installerCalled {
		t.Fatalf("exit=%d installerCalled=%v, want refusal before installer", exitCode, installerCalled)
	}
	if !strings.Contains(stderr.String(), "not a released standalone installation") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunUpdateRefusesUnmanagedPathBeforeInstaller(t *testing.T) {
	originalCurrent := currentSwobuVersion
	currentSwobuVersion = func() string { return "v1.2.3" }
	t.Cleanup(func() { currentSwobuVersion = originalCurrent })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	installerCalled := false
	var stderr bytes.Buffer
	exitCode := runUpdate(context.Background(), &bytes.Buffer{}, &stderr, nil, Runner{
		UpdateExecutable: func() (string, error) {
			return filepath.Join(home, "bin", "swobu"), nil
		},
		RunInstaller: func(context.Context, *http.Client, string, io.Writer, io.Writer) error {
			installerCalled = true
			return nil
		},
	})
	if exitCode != ExitDown || installerCalled {
		t.Fatalf("exit=%d installerCalled=%v, want ownership refusal", exitCode, installerCalled)
	}
	if !strings.Contains(stderr.String(), "not managed by `swobu update`") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunUpdateInvokesCanonicalUnixInstaller(t *testing.T) {
	originalCurrent := currentSwobuVersion
	currentSwobuVersion = func() string { return "1.2.3" }
	t.Cleanup(func() { currentSwobuVersion = originalCurrent })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var gotURL string
	var gotClient *http.Client
	exitCode := runUpdate(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil, Runner{
		UpdateExecutable: func() (string, error) {
			return filepath.Join(home, ".local", "bin", "swobu"), nil
		},
		RunInstaller: func(_ context.Context, client *http.Client, installerURL string, _ io.Writer, _ io.Writer) error {
			gotClient = client
			gotURL = installerURL
			return nil
		},
	})
	if exitCode != ExitHealthy || gotURL != unixInstallerURL {
		t.Fatalf("exit=%d url=%q, want healthy and %q", exitCode, gotURL, unixInstallerURL)
	}
	if gotClient == nil || gotClient.Timeout != 0 {
		t.Fatalf("update client timeout=%v, want no whole-download timeout", gotClient.Timeout)
	}
}

func TestRunUpdateReportsInstallerFailure(t *testing.T) {
	originalCurrent := currentSwobuVersion
	currentSwobuVersion = func() string { return "v1.2.3" }
	t.Cleanup(func() { currentSwobuVersion = originalCurrent })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	exitCode := runUpdate(context.Background(), &bytes.Buffer{}, &stderr, nil, Runner{
		UpdateExecutable: func() (string, error) {
			return filepath.Join(home, ".local", "bin", "swobu"), nil
		},
		RunInstaller: func(context.Context, *http.Client, string, io.Writer, io.Writer) error {
			return errors.New("installer failed")
		},
	})
	if exitCode != ExitDown || !strings.Contains(stderr.String(), "update failed: installer failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestManagedInstallTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		goos         string
		home         string
		localAppData string
		wantPath     string
		wantURL      string
	}{
		{name: "linux", goos: "linux", home: "/home/user", wantPath: filepath.Join("/home/user", ".local", "bin", "swobu"), wantURL: unixInstallerURL},
		{name: "windows", goos: "windows", localAppData: `C:\Users\user\AppData\Local`, wantPath: filepath.Join(`C:\Users\user\AppData\Local`, "Programs", "swobu", "bin", "swobu.exe"), wantURL: windowsInstallerURL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotPath, gotURL, err := managedInstallTarget(test.goos, test.home, test.localAppData)
			if err != nil || gotPath != test.wantPath || gotURL != test.wantURL {
				t.Fatalf("target=(%q,%q,%v), want=(%q,%q,nil)", gotPath, gotURL, err, test.wantPath, test.wantURL)
			}
		})
	}
}
