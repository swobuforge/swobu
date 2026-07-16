package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

var errVersionNoticeAcknowledgmentUnavailable = errors.New("version notice acknowledgment unavailable")

func waitForVersionNoticeContinue(in io.Reader, out io.Writer) error {
	if in == nil {
		return fmt.Errorf("%w: stdin missing", errVersionNoticeAcknowledgmentUnavailable)
	}
	_, _ = fmt.Fprintln(out, "press Enter to continue")
	reader := bufio.NewReader(in)
	if _, err := reader.ReadBytes('\n'); err != nil {
		return fmt.Errorf("%w: %v", errVersionNoticeAcknowledgmentUnavailable, err)
	}
	return nil
}
