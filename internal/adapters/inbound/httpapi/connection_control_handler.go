package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/profile"
)

// NewConnectionControlHandler serves canonical lifecycle and check commands. Mount on
// the admitted local-control listener behind BrowserSessions; this handler also
// requires trusted operator context, never inferring authority from localhost.
func NewConnectionControlHandler(queries *connectionapp.Queries, commands *connectionapp.Commands, writer connectionapp.ReplacementWriter, checkProbe connectionapp.Probe) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/connections/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
		id, ok := connectionPathID(w, r)
		if !ok {
			return
		}
		raw, problem := StrongRevision(r.Header)
		if problem != nil {
			WriteControlProblem(w, r, *problem)
			return
		}
		expected, err := domain.ParseRevision(raw)
		if err != nil {
			connectionProblem(w, r, 400, "INVALID_PRECONDITION", "Supply a valid Connection revision.")
			return
		}
		// Checks have no request configuration: the reviewed stored snapshot is
		// the only authority. Bound and reject any supplied body before effects.
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1))
			if err != nil || len(body) != 0 {
				connectionProblem(w, r, 400, "INVALID_ARGUMENT", "Connection checks do not accept a request body.")
				return
			}
		}
		if commands == nil || checkProbe == nil {
			connectionProblem(w, r, 503, "UNAVAILABLE", "Connection checks are unavailable.")
			return
		}
		check, err := commands.Check(r.Context(), id, expected, func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !IsLocalOperatorRequest(r) {
				return errLocalAdmissionForbidden
			}
			return nil
		}, checkProbe)
		if err == nil {
			connectionJSON(w, check, "")
			return
		}
		switch {
		case errors.Is(err, errLocalAdmissionForbidden):
			connectionProblem(w, r, 403, "FORBIDDEN", "Trusted operator context required.")
		case errors.Is(err, domain.ErrRevisionConflict):
			problem := *contractProblem(412, "REVISION_CONFLICT", "revision-conflict", "The Connection changed after review.")
			problem.Recovery = &ProblemRecovery{Action: "reload"}
			current, readErr := queries.Get(r.Context(), id)
			var summary *connectionapp.Connection
			if readErr == nil {
				summary = &current
			}
			writeConnectionMutationProblem(w, r, problem, nil, summary)
		default:
			connectionReadError(w, r, err)
		}
	})
	mux.HandleFunc("GET /api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || !connectionQueryValid(values, "kind", "q") {
			connectionProblem(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid Connection filter.")
			return
		}
		kind := domain.Kind(values.Get("kind"))
		if kind != "" && kind != domain.Model && kind != domain.MCP && kind != domain.Search {
			connectionProblem(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid Connection kind.")
			return
		}
		result, err := queries.List(r.Context(), connectionapp.Filter{Kind: kind, Query: values.Get("q")})
		if err != nil {
			connectionReadError(w, r, err)
			return
		}
		connectionJSON(w, result, "")
	})
	mux.HandleFunc("GET /api/v1/connections/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := connectionPathID(w, r)
		if !ok {
			return
		}
		result, err := queries.Get(r.Context(), id)
		if err != nil {
			connectionReadError(w, r, err)
			return
		}
		connectionJSON(w, result, result.Revision)
	})
	mux.HandleFunc("GET /api/v1/connections/{id}/references", func(w http.ResponseWriter, r *http.Request) {
		id, ok := connectionPathID(w, r)
		if !ok {
			return
		}
		result, err := queries.References(r.Context(), id)
		if err != nil {
			connectionReadError(w, r, err)
			return
		}
		// Consumers change independently of Connection revision; only detail
		// readback carries the resource ETag used for configuration preconditions.
		connectionJSON(w, result, "")
	})
	mux.HandleFunc("DELETE /api/v1/connections/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := connectionPathID(w, r)
		if !ok {
			return
		}
		raw, problem := StrongRevision(r.Header)
		if problem != nil {
			WriteControlProblem(w, r, *problem)
			return
		}
		expected, err := domain.ParseRevision(raw)
		if err != nil {
			connectionProblem(w, r, http.StatusBadRequest, "INVALID_PRECONDITION", "Supply a valid Connection revision.")
			return
		}
		if commands == nil {
			connectionProblem(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Connection commands are unavailable.")
			return
		}
		err = commands.Delete(r.Context(), id, expected, "local", func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !IsLocalOperatorRequest(r) {
				return errLocalAdmissionForbidden
			}
			return nil
		})
		if err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var inUse *domain.InUseError
		switch {
		case errors.Is(err, errLocalAdmissionForbidden):
			connectionProblem(w, r, http.StatusForbidden, "FORBIDDEN", "Trusted operator context required.")
		case errors.Is(err, domain.ErrRevisionConflict):
			problem := *contractProblem(http.StatusPreconditionFailed, "REVISION_CONFLICT", "revision-conflict", "The Connection changed after review.")
			problem.Recovery = &ProblemRecovery{Action: "reload"}
			current, readErr := queries.Get(r.Context(), id)
			var summary *connectionapp.Connection
			if readErr == nil {
				summary = &current
			}
			writeConnectionMutationProblem(w, r, problem, nil, summary)
		case errors.As(err, &inUse):
			consumers := make([]connectionapp.Reference, 0, len(inUse.Consumers()))
			for _, consumer := range inUse.Consumers() {
				consumers = append(consumers, connectionapp.Reference{Workspace: consumer.Workspace, Kind: consumer.Kind, Route: consumer.Route, Target: consumer.Target, Mount: consumer.Mount})
			}
			writeConnectionMutationProblem(w, r, *contractProblem(http.StatusConflict, "CONNECTION_IN_USE", "connection-in-use", "Remove the listed references before deleting this Connection."), consumers, nil)
		default:
			connectionReadError(w, r, err)
		}
	})

	mux.HandleFunc("POST /api/v1/connections/{id}/credential-replacements", func(w http.ResponseWriter, r *http.Request) {
		id, ok := connectionPathID(w, r)
		if !ok {
			return
		}
		revision, problem := StrongRevision(r.Header)
		if problem != nil {
			WriteControlProblem(w, r, *problem)
			return
		}
		expected, err := domain.ParseRevision(revision)
		if err != nil {
			connectionProblem(w, r, 400, "INVALID_PRECONDITION", "Supply a valid Connection revision.")
			return
		}
		key, problem := IdempotencyKey(r.Header)
		if problem != nil {
			WriteControlProblem(w, r, *problem)
			return
		}
		var body connectionCredentialReplacement
		if err := decodeOperatorJSONObject(w, r, &body, "credential replacement"); err != nil {
			connectionProblem(w, r, operatorJSONErrorStatus(err), "INVALID_ARGUMENT", "Supply one valid credential replacement object.")
			return
		}
		if commands == nil || writer == nil {
			connectionProblem(w, r, 503, "UNAVAILABLE", "Connection commands are unavailable.")
			return
		}
		result, replayed, err := commands.ReplaceCredential(r.Context(), id, expected, body.Environment, body.Secret, body.SameIdentity, "local", key, func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !IsLocalOperatorRequest(r) {
				return errLocalAdmissionForbidden
			}
			return nil
		}, writer)
		if err != nil {
			switch {
			case errors.Is(err, errLocalAdmissionForbidden):
				connectionProblem(w, r, 403, "FORBIDDEN", "Trusted operator context required.")
			case errors.Is(err, domain.ErrRevisionConflict):
				problem := *contractProblem(412, "REVISION_CONFLICT", "revision-conflict", "The Connection changed after review.")
				problem.Recovery = &ProblemRecovery{Action: "reload"}
				current, readErr := queries.Get(r.Context(), id)
				var summary *connectionapp.Connection
				if readErr == nil {
					summary = &current
				}
				writeConnectionMutationProblem(w, r, problem, nil, summary)
			case errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, connectionapp.ErrMaterialConflict):
				connectionProblem(w, r, 409, "IDEMPOTENCY_CONFLICT", "Retry key was used with different replacement intent.")
			case errors.Is(err, domain.ErrNotFound):
				connectionProblem(w, r, 404, "NOT_FOUND", "Connection not found.")
			case errors.Is(err, connectionapp.ErrIdentityAssertion), errors.Is(err, domain.ErrCredentialBinding), strings.TrimSpace(body.Secret) == "":
				connectionProblem(w, r, 400, "INVALID_ARGUMENT", "Confirm unchanged identity and select an existing credential binding with nonempty material.")
			default:
				connectionProblem(w, r, 503, "UNAVAILABLE", "Credential replacement could not complete; retry the original request.")
			}
			return
		}
		connectionJSON(w, struct {
			Connection connectionapp.Connection `json:"connection"`
			Replayed   bool                     `json:"replayed"`
		}{result, replayed}, result.Revision)
	})

	for _, operation := range []string{"POST /api/v1/connections", "PATCH /api/v1/connections/{id}"} {
		mux.HandleFunc(operation, func(w http.ResponseWriter, r *http.Request) {
			var id connectionidentity.ID
			var expected domain.Revision
			var key string
			if r.Method == http.MethodPatch {
				var ok bool
				id, ok = connectionPathID(w, r)
				if !ok {
					return
				}
				raw, problem := StrongRevision(r.Header)
				if problem != nil {
					WriteControlProblem(w, r, *problem)
					return
				}
				var err error
				expected, err = domain.ParseRevision(raw)
				if err != nil {
					connectionProblem(w, r, 400, "INVALID_PRECONDITION", "Supply a valid Connection revision.")
					return
				}
			} else {
				if r.URL.RawQuery != "" {
					connectionProblem(w, r, 400, "INVALID_ARGUMENT", "Query parameters are not accepted.")
					return
				}
				var problem *ControlProblem
				key, problem = IdempotencyKey(r.Header)
				if problem != nil {
					WriteControlProblem(w, r, *problem)
					return
				}
			}
			var draft connectionapp.Draft
			if err := decodeOperatorJSONObject(w, r, &draft, "Connection draft"); err != nil {
				connectionProblem(w, r, operatorJSONErrorStatus(err), "INVALID_ARGUMENT", "Supply one complete valid Connection draft.")
				return
			}
			if commands == nil {
				connectionProblem(w, r, 503, "UNAVAILABLE", "Connection commands are unavailable.")
				return
			}
			spec, err := draft.ValidatedSpec(profile.SupportsSpec)
			if err != nil {
				connectionProblem(w, r, 400, "INVALID_ARGUMENT", "Connection spec is invalid or unsupported.")
				return
			}
			admit := func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !IsLocalOperatorRequest(r) {
					return errLocalAdmissionForbidden
				}
				return nil
			}
			var result connectionapp.Connection
			replayed := false
			if r.Method == http.MethodPost {
				result, replayed, err = commands.Create(r.Context(), draft.DisplayName, spec, "local", key, admit)
			} else {
				result, err = commands.EditSameIdentity(r.Context(), id, expected, draft.DisplayName, spec, draft.SameIdentity, "local", admit)
			}
			if err != nil {
				switch {
				case errors.Is(err, errLocalAdmissionForbidden):
					connectionProblem(w, r, 403, "FORBIDDEN", "Trusted operator context required.")
				case errors.Is(err, domain.ErrRevisionConflict):
					problem := *contractProblem(412, "REVISION_CONFLICT", "revision-conflict", "The Connection changed after review.")
					problem.Recovery = &ProblemRecovery{Action: "reload"}
					current, readErr := queries.Get(r.Context(), id)
					var summary *connectionapp.Connection
					if readErr == nil {
						summary = &current
					}
					writeConnectionMutationProblem(w, r, problem, nil, summary)
				case errors.Is(err, domain.ErrNotFound):
					connectionProblem(w, r, 404, "NOT_FOUND", "Connection not found.")
				case errors.Is(err, domain.ErrIdempotencyConflict):
					connectionProblem(w, r, 409, "IDEMPOTENCY_CONFLICT", "Retry key was used with different intent.")
				case errors.Is(err, connectionapp.ErrIdentityAssertion), errors.Is(err, domain.ErrIdentityChange):
					connectionProblem(w, r, 400, "INVALID_ARGUMENT", "Confirm unchanged external identity; use a new Connection for another identity.")
				default:
					connectionProblem(w, r, 503, "UNAVAILABLE", "Connection draft could not be published; retry the original intent.")
				}
				return
			}
			if r.Method == http.MethodPost {
				w.Header().Set("Location", "/api/v1/connections/"+result.ID)
				w.Header().Set("ETag", `"`+result.Revision+`"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(struct {
					Connection connectionapp.Connection `json:"connection"`
					Replayed   bool                     `json:"replayed"`
				}{result, replayed})
			} else {
				connectionJSON(w, result, result.Revision)
			}
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !IsLocalOperatorRequest(r) {
			connectionProblem(w, r, http.StatusForbidden, "FORBIDDEN", "Trusted operator context required.")
			return
		}
		if queries == nil {
			connectionProblem(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Connection queries are unavailable.")
			return
		}
		// GET registers HEAD too; DELETE is supported only on resource detail.
		probe := r.Clone(r.Context())
		probe.Method = http.MethodGet
		_, readPattern := mux.Handler(probe)
		detail := readPattern == "GET /api/v1/connections/{id}"
		replacement := strings.HasSuffix(r.URL.Path, "/credential-replacements")
		checking := strings.HasSuffix(r.URL.Path, "/checks")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !(r.Method == http.MethodDelete && detail) && !(r.Method == http.MethodPost && (replacement || checking || r.URL.Path == "/api/v1/connections")) && !(r.Method == http.MethodPatch && detail) {
			allow := "GET, HEAD"
			if detail {
				allow += ", PATCH, DELETE"
			}
			w.Header().Set("Allow", allow)
			connectionProblem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Unsupported Connection operation.")
			return
		}
		_, pattern := mux.Handler(r)
		if pattern == "" {
			connectionProblem(w, r, http.StatusNotFound, "NOT_FOUND", "Connection operation not found.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func connectionQueryValid(values url.Values, allowed ...string) bool {
	for key, entries := range values {
		found := false
		for _, candidate := range allowed {
			if key == candidate {
				found = true
				break
			}
		}
		if !found || len(entries) != 1 {
			return false
		}
	}
	return true
}

func connectionPathID(w http.ResponseWriter, r *http.Request) (connectionidentity.ID, bool) {
	id, err := connectionidentity.Parse(r.PathValue("id"))
	if err != nil || r.URL.RawQuery != "" {
		connectionProblem(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "A valid Connection ID without query parameters is required.")
		return connectionidentity.ID{}, false
	}
	return id, true
}

func connectionReadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		connectionProblem(w, r, http.StatusNotFound, "NOT_FOUND", "Connection not found.")
		return
	}
	connectionProblem(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Connection read failed.")
}

func connectionProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	WriteControlProblem(w, r, *contractProblem(status, code, strings.ToLower(strings.ReplaceAll(code, "_", "-")), detail))
}

func connectionJSON(w http.ResponseWriter, value any, revision string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if revision != "" {
		w.Header().Set("ETag", `"`+revision+`"`)
	}
	_ = json.NewEncoder(w).Encode(value)
}

// Blockers come from the locked delete transaction; a later reference query
// could describe different consumers. Current is advisory reload information.
func writeConnectionMutationProblem(w http.ResponseWriter, r *http.Request, problem ControlProblem, consumers []connectionapp.Reference, current *connectionapp.Connection) {
	problem.Instance = r.URL.Path
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(struct {
		ControlProblem
		Consumers []connectionapp.Reference `json:"consumers,omitempty"`
		Current   *connectionapp.Connection `json:"current,omitempty"`
	}{problem, consumers, current})
}

// This secret-bearing command is closed: duplicate, null and unknown members
// cannot silently change the operator's retry intent.
type connectionCredentialReplacement struct {
	Secret       string `json:"secret"`
	Environment  string `json:"environment,omitempty"`
	SameIdentity bool   `json:"same_identity"`
}

func (body *connectionCredentialReplacement) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("invalid replacement object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return errors.New("duplicate replacement field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null replacement field")
		}
		switch name {
		case "secret":
			err = json.Unmarshal(value, &body.Secret)
		case "environment":
			err = json.Unmarshal(value, &body.Environment)
		case "same_identity":
			err = json.Unmarshal(value, &body.SameIdentity)
		default:
			return errors.New("unknown replacement field")
		}
		if err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if !seen["secret"] || !seen["same_identity"] {
		return errors.New("missing replacement field")
	}
	return nil
}
