package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxOperatorJSONBodyBytes reuses the shipped control-message deployment
// envelope. Operator requests contain configuration and credentials, not the
// media payloads admitted by the larger inference-request envelope.
const maxOperatorJSONBodyBytes = 1 << 20

var errOperatorJSONContentType = errors.New("Content-Type must be application/json")

// decodeOperatorJSONObject establishes the HTTP memory envelope before the
// transport-agnostic decoder observes extensible object members.
func decodeOperatorJSONObject(w http.ResponseWriter, req *http.Request, target any, surface string) error {
	value := strings.TrimSpace(req.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return errOperatorJSONContentType
	}
	if req.Body == nil {
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	limited := http.MaxBytesReader(w, req.Body, maxOperatorJSONBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("%s body exceeds maximum allowed size", surface)
		}
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	return decodeOperatorJSONBytes(raw, target, surface)
}

func operatorJSONErrorStatus(err error) int {
	if errors.Is(err, errOperatorJSONContentType) {
		return http.StatusUnsupportedMediaType
	}
	return http.StatusBadRequest
}

func decodeOperatorJSONBytes(raw []byte, target any, surface string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s body is invalid JSON", surface)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("%s body must be a JSON object", surface)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
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
