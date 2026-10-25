package atomicfile

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const crashOldDocument = `{"revision":1,"authority":"old"}`
const crashNewDocument = `{"revision":2,"authority":"new"}`

func TestAtomicReplacementCrashHelper(t *testing.T) {
	path := os.Getenv("SWOBU_ATOMIC_CRASH_PATH")
	if path == "" {
		return
	}
	stage := os.Getenv("SWOBU_ATOMIC_CRASH_STAGE")
	stop := func(point string) {
		if point == stage {
			fmt.Fprintln(os.Stdout, "ATOMIC_BOUNDARY_READY")
			_, _ = os.Stdin.Read(make([]byte, 1))
		}
	}
	operations := DefaultOperations()
	create, write, syncFile, rename, syncDir := operations.CreateTemp, operations.Write, operations.SyncFile, operations.Rename, operations.SyncDir
	operations.CreateTemp = func(dir, pattern string) (*os.File, error) {
		file, err := create(dir, pattern)
		if err == nil {
			stop("created")
		}
		return file, err
	}
	operations.Write = func(file *os.File, raw []byte) (int, error) {
		if stage == "partial-write" {
			count, err := write(file, raw[:len(raw)/2])
			if err == nil {
				stop("partial-write")
			}
			return count, err
		}
		count, err := write(file, raw)
		if err == nil {
			stop("written")
		}
		return count, err
	}
	operations.SyncFile = func(file *os.File) error {
		err := syncFile(file)
		if err == nil {
			stop("file-synced")
		}
		return err
	}
	operations.Rename = func(from, to string) error {
		stop("before-rename")
		err := rename(from, to)
		if err == nil {
			stop("renamed")
		}
		return err
	}
	operations.SyncDir = func(dir string) error {
		stop("before-directory-sync")
		err := syncDir(dir)
		if err == nil {
			stop("directory-synced")
		}
		return err
	}
	if err := ReplaceWith(path, []byte(crashNewDocument), 0o600, operations); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicReplacementProcessDeathPreservesCompleteAuthorityAtWriteBoundaries(t *testing.T) {
	for _, stage := range []string{"created", "partial-write", "written", "file-synced", "before-rename", "renamed", "before-directory-sync", "directory-synced"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "authority.json")
			if err := Replace(path, []byte(crashOldDocument), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAtomicReplacementCrashHelper$", "-test.count=1")
			command.Env = append(os.Environ(), "SWOBU_ATOMIC_CRASH_PATH="+path, "SWOBU_ATOMIC_CRASH_STAGE="+stage)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if scanner.Text() == "ATOMIC_BOUNDARY_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child failed before selected boundary")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child did not reach selected boundary")
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("child did not die abruptly")
			}
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := crashOldDocument
			if stage == "renamed" || stage == "before-directory-sync" || stage == "directory-synced" {
				want = crashNewDocument
			}
			if string(actual) != want {
				t.Fatalf("partial/wrong authority after %s: %s", stage, actual)
			}
			if err := Replace(path, []byte(crashNewDocument), 0o600); err != nil {
				t.Fatal(err)
			}
			recovered, err := os.ReadFile(path)
			if err != nil || string(recovered) != crashNewDocument {
				t.Fatal("subsequent replacement selected abandoned staging data")
			}
		})
	}
}
