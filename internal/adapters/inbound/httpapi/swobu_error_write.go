package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type swobuErrorEnvelope struct {
	Error swobuErrorBody `json:"error"`
}

type swobuErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func statusCodeForSwobuError(code canonical.ErrorCode) int {
	switch code {
	case canonical.ErrorCodeBadRequest:
		return http.StatusBadRequest
	case canonical.ErrorCodeBadEndpoint, canonical.ErrorCodeUnsupportedEndpoint, canonical.ErrorCodeUnknownTarget:
		return http.StatusNotFound
	case canonical.ErrorCodeUnsupportedOperation, canonical.ErrorCodeUnsupportedDelivery:
		return http.StatusBadRequest
	case canonical.ErrorCodeNotImplemented:
		return http.StatusNotImplemented
	case canonical.ErrorCodeNoAvailableTarget:
		return http.StatusServiceUnavailable
	case canonical.ErrorCodeProviderTimeout:
		return http.StatusGatewayTimeout
	case canonical.ErrorCodeProviderUnavailable:
		return http.StatusServiceUnavailable
	case canonical.ErrorCodeOperationNotConfigured:
		return http.StatusBadRequest
	case canonical.ErrorCodeRequestRateLimited:
		return http.StatusTooManyRequests
	case canonical.ErrorCodeCostUnavailable, canonical.ErrorCodeBudgetExceeded:
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}

func writeSwobuError(w http.ResponseWriter, err canonical.Error) {
	writeAdmissionRetryHeaders(w, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCodeForSwobuError(err.Code))
	_ = json.NewEncoder(w).Encode(swobuErrorEnvelope{Error: swobuErrorBody{Code: string(err.Code), Message: err.Message}})
}

func writeAdmissionRetryHeaders(w http.ResponseWriter, err canonical.Error) {
	if err.Code != canonical.ErrorCodeRequestRateLimited {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err.RetryAfter > 0 {
		seconds := (err.RetryAfter + time.Second - 1) / time.Second
		w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
	}
}
