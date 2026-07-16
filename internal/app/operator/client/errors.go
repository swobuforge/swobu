package operatorclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ResponseError captures a structured non-2xx daemon response.
type ResponseError struct {
	StatusCode int
	Code       string
	Message    string
	Fallback   string
}

func (e *ResponseError) Error() string {
	if e == nil {
		return "operator client: unexpected response error"
	}
	if message := strings.TrimSpace(e.Message); message != "" {
		code := strings.TrimSpace(e.Code)
		if code != "" {
			return fmt.Sprintf("operator client: %s (code=%s)", message, code)
		}
		return fmt.Sprintf("operator client: %s", message)
	}
	if fallback := strings.TrimSpace(e.Fallback); fallback != "" {
		return fmt.Sprintf("%s returned status %d", fallback, e.StatusCode)
	}
	return fmt.Sprintf("operator client: returned status %d", e.StatusCode)
}

// IsNotFound reports whether err represents a structured daemon 404 response.
func IsNotFound(err error) bool {
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr == nil {
		return false
	}
	return responseErr.StatusCode == http.StatusNotFound || strings.EqualFold(strings.TrimSpace(responseErr.Code), "NOT_FOUND")
}
