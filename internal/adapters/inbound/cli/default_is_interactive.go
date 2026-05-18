package cli

import (
	"os"

	"golang.org/x/term"
)

func defaultIsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
