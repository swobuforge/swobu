package trafficevidence

import "fmt"

type ResultClass string

const (
	ResultClassInProgress                 ResultClass = "in_progress"
	ResultClassSuccess                    ResultClass = "success"
	ResultClassSwobuError                 ResultClass = "swobu_error"
	ResultClassBackendError               ResultClass = "backend_error"
	ResultClassCancelled                  ResultClass = "cancelled"
	ResultClassUnsupportedOperation       ResultClass = "unsupported_operation"
	ResultClassUnsupportedDeliveryVariant ResultClass = "unsupported_delivery_variant"
	ResultClassNotImplemented             ResultClass = "not_implemented"
)

func ParseResultClass(raw string) (ResultClass, error) {
	switch value := ResultClass(raw); value {
	case ResultClassInProgress,
		ResultClassSuccess,
		ResultClassSwobuError,
		ResultClassBackendError,
		ResultClassCancelled,
		ResultClassUnsupportedOperation,
		ResultClassUnsupportedDeliveryVariant,
		ResultClassNotImplemented:
		return value, nil
	default:
		return "", fmt.Errorf("unknown result class %q", raw)
	}
}

func (r ResultClass) String() string {
	return string(r)
}

func (r ResultClass) IsTerminal() bool {
	return r != "" && r != ResultClassInProgress
}
