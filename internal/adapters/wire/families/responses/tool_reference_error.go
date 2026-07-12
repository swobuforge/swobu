package responses

import (
	"errors"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// wrapResponsesToolReferenceError makes flat tool-reference failures
// self-identifying without weakening the fail-closed projection contract.
func wrapResponsesToolReferenceError(fieldPath, kindDescriptor, name string, err error) error {
	if err == nil {
		return nil
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) || compatErr.Code != canonical.ErrorCodeBadRequest {
		return err
	}
	kindDescriptor = strings.TrimSpace(kindDescriptor)
	name = strings.TrimSpace(name)
	fieldPath = strings.TrimSpace(fieldPath)
	if fieldPath == "" || kindDescriptor == "" || name == "" {
		return err
	}
	cause := strings.TrimSpace(compatErr.Message)
	compatErr.Details = map[string]string{
		"request_field": fieldPath,
		"tool_kind":     kindDescriptor,
		"tool_name":     name,
	}
	if cause == "" {
		compatErr.Message = fmt.Sprintf("responses request %s (%s) name %q is invalid", fieldPath, kindDescriptor, name)
		return compatErr
	}
	compatErr.Message = fmt.Sprintf("responses request %s (%s) name %q is invalid: %s", fieldPath, kindDescriptor, name, cause)
	return compatErr
}
