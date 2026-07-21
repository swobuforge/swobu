package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// DecodeExtensibleRequestObject applies the public unknown-member contract at
// a typed protocol request envelope and maps shape failures to wire truth.
func DecodeExtensibleRequestObject(raw []byte, out any, surface string) error {
	if err := decodeRequestObject(raw, out, surface); err != nil {
		return canonical.BadRequest(err.Error())
	}
	return nil
}

func decodeRequestObject(raw []byte, out any, surface string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("%s body must be a JSON object", surface)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("%s body contains trailing data", surface)
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	return nil
}
