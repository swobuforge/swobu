package adapters

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
)

type runCommandIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type runCommandExecutor func(context.Context, clientprofile.RunCommandSpec) error

func processRunCommandIO() runCommandIO {
	return runCommandIO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

func executeClientRunCommand(ctx context.Context, command clientprofile.RunCommandSpec, ioCfg runCommandIO) error {
	if strings.TrimSpace(command.Binary) == "" { // swobu:io-string source=boundary
		return errors.New("run command binary is required")
	}
	if command.Prepare != nil {
		if err := prepareRunCommandFile(*command.Prepare); err != nil {
			return err
		}
	}
	ioCfg = normalizeRunCommandIO(ioCfg)
	cmd := exec.CommandContext(ctx, command.Binary, command.Args...)
	cmd.Stdin = ioCfg.Stdin
	cmd.Stdout = ioCfg.Stdout
	cmd.Stderr = ioCfg.Stderr
	cmd.Env = runCommandEnvironment(command.Env)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func normalizeRunCommandIO(ioCfg runCommandIO) runCommandIO {
	if ioCfg.Stdin == nil {
		ioCfg.Stdin = os.Stdin
	}
	if ioCfg.Stdout == nil {
		ioCfg.Stdout = os.Stdout
	}
	if ioCfg.Stderr == nil {
		ioCfg.Stderr = os.Stderr
	}
	return ioCfg
}

func prepareRunCommandFile(spec clientprofile.RunPrepareFileSpec) error {
	path := strings.TrimSpace(spec.Path) // swobu:io-string source=boundary
	if path == "" {
		return errors.New("run prepare path is required")
	}
	if spec.WriteIfMissing {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	mode := spec.Mode
	if mode == 0 {
		mode = 0o644
	}
	return os.WriteFile(path, []byte(spec.Content), mode)
}

func runCommandEnvironment(overrides map[string]string) []string {
	return sortEnv(mergedEnv(overrides))
}

func mergedEnv(overrides map[string]string) map[string]string {
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func sortEnv(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}
