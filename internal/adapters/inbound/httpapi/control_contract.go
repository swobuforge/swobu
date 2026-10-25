package httpapi

import (
	"encoding/json"
	"net/http"
)

// ControlProblem is the RFC 002 error envelope for canonical control APIs.
// Callers supply operator-safe detail, never provider bodies or credential values.
type ControlProblem struct {
	Type     string            `json:"type"`
	Title    string            `json:"title"`
	Status   int               `json:"status"`
	Code     string            `json:"code"`
	Detail   string            `json:"detail,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
	Recovery *ProblemRecovery  `json:"recovery,omitempty"`
}

type ProblemRecovery struct {
	Action string `json:"action"`
}

// WriteControlProblem never echoes request bodies, queries or headers. The
// instance is a path only so launch tokens cannot enter error representations.
func WriteControlProblem(w http.ResponseWriter, r *http.Request, problem ControlProblem) {
	problem.Instance = r.URL.Path
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}

func contractProblem(status int, code, kind, detail string) *ControlProblem {
	return &ControlProblem{
		Type: "https://swobu.local/problems/" + kind, Title: http.StatusText(status),
		Status: status, Code: code, Detail: detail,
	}
}

// StrongRevision requires one exact strong entity tag. It returns the opaque
// revision without quotes; application transactions own comparison and locking.
func StrongRevision(header http.Header) (string, *ControlProblem) {
	values := header.Values("If-Match")
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		return "", contractProblem(http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "precondition-required", "If-Match is required.")
	}
	if len(values) != 1 || !validStrongETag(values[0]) {
		return "", contractProblem(http.StatusBadRequest, "INVALID_PRECONDITION", "invalid-precondition", "Supply one exact strong If-Match entity tag.")
	}
	return values[0][1 : len(values[0])-1], nil
}

func validStrongETag(value string) bool {
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	// Reject whitespace, separators, escapes and lists rather than silently
	// changing the reviewed identity. Domain owners choose the revision format.
	for _, char := range value[1 : len(value)-1] {
		if char < 0x21 || char > 0x7e || char == '"' || char == '\\' || char == ',' {
			return false
		}
	}
	return true
}

// IdempotencyKey parses the wire key only. Durable replay and semantic conflict
// checks belong inside each owning application's publication transaction.
func IdempotencyKey(header http.Header) (string, *ControlProblem) {
	values := header.Values("Idempotency-Key")
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 256 {
		return "", contractProblem(http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "invalid-idempotency-key", "Supply one Idempotency-Key of 1–256 visible ASCII bytes.")
	}
	for _, char := range values[0] {
		if char < 0x21 || char > 0x7e {
			return "", contractProblem(http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "invalid-idempotency-key", "Idempotency-Key must contain only visible ASCII bytes.")
		}
	}
	return values[0], nil
}
