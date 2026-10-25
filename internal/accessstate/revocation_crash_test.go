package accessstate

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
	"github.com/swobuforge/swobu/internal/routing"
)

const (
	accessRevokeCrashPathEnv  = "SWOBU_ACCESS_REVOKE_CRASH_PATH"
	accessRevokeCrashStageEnv = "SWOBU_ACCESS_REVOKE_CRASH_STAGE"
)

func TestWorkspaceRevocationCrashHelper(t *testing.T) {
	path := os.Getenv(accessRevokeCrashPathEnv)
	if path == "" {
		return
	}
	stage := os.Getenv(accessRevokeCrashStageEnv)
	operations := atomicfile.DefaultOperations()
	pause := func(boundary string) {
		if boundary == stage {
			_, _ = fmt.Fprintln(os.Stdout, "ACCESS_WORKSPACE_REVOKE_BOUNDARY_READY")
			_, _ = os.Stdin.Read(make([]byte, 1))
		}
	}
	createTemp, write, syncFile, rename, syncDir := operations.CreateTemp, operations.Write, operations.SyncFile, operations.Rename, operations.SyncDir
	operations.CreateTemp = func(dir, pattern string) (*os.File, error) {
		file, err := createTemp(dir, pattern)
		if err == nil {
			pause("created")
		}
		return file, err
	}
	operations.Write = func(file *os.File, raw []byte) (int, error) {
		if stage == "partial-write" {
			count, err := write(file, raw[:len(raw)/2])
			if err == nil {
				pause("partial-write")
			}
			return count, err
		}
		count, err := write(file, raw)
		if err == nil {
			pause("written")
		}
		return count, err
	}
	operations.SyncFile = func(file *os.File) error {
		err := syncFile(file)
		if err == nil {
			pause("file-synced")
		}
		return err
	}
	operations.Rename = func(from, to string) error {
		pause("before-rename")
		if err := rename(from, to); err != nil {
			return err
		}
		pause("renamed")
		return nil
	}
	operations.SyncDir = func(dir string) error {
		pause("before-directory-sync")
		if err := syncDir(dir); err != nil {
			return err
		}
		pause("directory-synced")
		return nil
	}
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// The Store's replacement primitive is centralized in commit. The child
	// uses the same atomic-file implementation with pauses at its real write
	// boundaries, then exercises the production Workspace revocation method.
	store.publish = func(raw []byte) error {
		return atomicfile.ReplaceWith(path, raw, 0o600, operations)
	}
	if _, err := store.RevokeWorkspace(testWorkspace(t, "research")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRevocationProcessDeathPreservesScopeAtPublicationBoundaries(t *testing.T) {
	for _, stage := range []string{"created", "partial-write", "written", "file-synced", "before-rename", "renamed", "before-directory-sync", "directory-synced"} {
		t.Run(stage, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "principals.json")
			store, err := openTestStore(path)
			if err != nil {
				t.Fatal(err)
			}
			research, err := routing.ParseWorkspaceSlug("research")
			if err != nil {
				t.Fatal(err)
			}
			coding, err := routing.ParseWorkspaceSlug("coding")
			if err != nil {
				t.Fatal(err)
			}
			principal, researchCredential, err := store.Create("consumer", research)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.GrantPermission(principal.ID, coding); err != nil {
				t.Fatal(err)
			}
			codingCredential, err := store.IssueCredential(principal.ID, coding)
			if err != nil {
				t.Fatal(err)
			}
			sibling, siblingCredential, err := store.Create("sibling", coding)
			if err != nil {
				t.Fatal(err)
			}
			if sibling.ID == principal.ID {
				t.Fatal("independent sibling Principal was not created")
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			command := exec.Command(os.Args[0], "-test.run=^TestWorkspaceRevocationCrashHelper$", "-test.count=1")
			command.Env = append(os.Environ(), accessRevokeCrashPathEnv+"="+path, accessRevokeCrashStageEnv+"="+stage)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if scanner.Text() == "ACCESS_WORKSPACE_REVOKE_BOUNDARY_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("revocation child failed before selected boundary")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("revocation child did not reach selected boundary")
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("revocation child survived process-death injection")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			reopened, err := openTestStore(path)
			if err != nil {
				t.Fatal(err)
			}
			wantResearchRevoked := stage == "renamed" || stage == "before-directory-sync" || stage == "directory-synced"
			if wantResearchRevoked == bytes.Equal(before, after) {
				t.Fatalf("document publication boundary mismatch at %s: committed=%t bytes_unchanged=%t", stage, wantResearchRevoked, bytes.Equal(before, after))
			}
			_, researchErr := reopened.Authenticate(researchCredential.Token)
			if wantResearchRevoked && researchErr == nil {
				t.Fatal("committed Workspace revocation restored research credential")
			}
			if !wantResearchRevoked && researchErr != nil {
				t.Fatalf("uncommitted Workspace revocation removed research credential: %v", researchErr)
			}
			if _, err := reopened.IssueCredential(principal.ID, research); wantResearchRevoked && !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("committed Workspace revocation allowed credential issuance: %v", err)
			} else if !wantResearchRevoked && err != nil {
				t.Fatalf("uncommitted Workspace revocation removed permission: %v", err)
			}
			if _, err := reopened.Authenticate(codingCredential.Token); err != nil {
				t.Fatalf("sibling Workspace credential on same Principal changed at %s: %v", stage, err)
			}
			if _, err := reopened.Authenticate(siblingCredential.Token); err != nil {
				t.Fatalf("unrelated sibling credential changed at %s: %v", stage, err)
			}
		})
	}
}
