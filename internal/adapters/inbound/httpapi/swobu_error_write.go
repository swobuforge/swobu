package httpapi

import (
	"encoding/json"
	"net/http"

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
	default:
		return http.StatusInternalServerError
	}
}

func writeSwobuError(w http.ResponseWriter, err canonical.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCodeForSwobuError(err.Code))
	_ = json.NewEncoder(w).Encode(swobuErrorEnvelope{Error: swobuErrorBody{Code: string(err.Code), Message: err.Message}})
}
