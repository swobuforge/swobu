package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

func waitForVersionNoticeContinue(in io.Reader, out io.Writer) error {
	if in == nil {
		return errors.New("version notice acknowledgment requires stdin")
	}
	_, _ = fmt.Fprintln(out, "press Enter to continue")
	reader := bufio.NewReader(in)
	if _, err := reader.ReadBytes('\n'); err != nil {
		return fmt.Errorf("version notice acknowledgment failed: %w", err)
	}
	return nil
}
