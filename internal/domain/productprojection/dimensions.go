// Package productprojection owns the bounded, lossy dimensions shared by the
// daemon's product telemetry projection. It is not execution lifecycle truth.
package productprojection

import "strings"

type ResultClass = string

const (
	ResultSuccess                    ResultClass = "success"
	ResultSwobuError                 ResultClass = "swobu_error"
	ResultBackendError               ResultClass = "backend_error"
	ResultCancelled                  ResultClass = "cancelled"
	ResultUnsupportedOperation       ResultClass = "unsupported_operation"
	ResultUnsupportedDeliveryVariant ResultClass = "unsupported_delivery_variant"
	ResultNotImplemented             ResultClass = "not_implemented"
)

func ResultFromCanonical(code string) ResultClass {
	switch strings.ToUpper(code) {
	case "NOT_IMPLEMENTED":
		return ResultNotImplemented
	case "UNSUPPORTED_OPERATION":
		return ResultUnsupportedOperation
	case "UNSUPPORTED_DELIVERY_MODE":
		return ResultUnsupportedDeliveryVariant
	default:
		return ResultSwobuError
	}
}
