package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
	"golang.org/x/term"
)

// ConfigureDefault installs the process-wide logger for daemon diagnostics.
// Invalid log-level configuration is a startup error so an operator cannot
// mistake silently downgraded INFO output for requested DEBUG diagnostics.
func ConfigureDefault(out *os.File) error {
	level, err := configuredLevel()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(newHandler(out, level, term.IsTerminal(int(out.Fd())))))
	return nil
}

func newHandler(out io.Writer, level slog.Leveler, interactive bool) slog.Handler {
	if interactive {
		// colorable is the presentation adapter recommended by tint for Windows;
		// semantic logging never depends on it outside this package.
		return tint.NewTextHandler(goColorable(out), &tint.Options{
			Level: level, AddSource: true, NoColor: os.Getenv("NO_COLOR") != "",
		})
	}
	return slog.NewTextHandler(out, &slog.HandlerOptions{Level: level, AddSource: true})
}

func goColorable(out io.Writer) io.Writer {
	if file, ok := out.(*os.File); ok {
		return colorable.NewColorable(file)
	}
	return out
}
