package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var errLocalAdmissionForbidden = errors.New("local daemon access is forbidden")

type admittedAuthority func(routing.WorkspaceSlug) (accessidentity.ID, error)
type admissionContextKey struct{}

// NewLocalAdmissionHandler protects the complete local daemon surface and
// resolves managed Access credentials before protocol decoding. Share runs on
// a separate public server and deliberately does not use this boundary.
func NewLocalAdmissionHandler(next http.Handler, state *accessstate.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := ValidateLocalAdmission(r); err != nil {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		authority, status, err := resolveRequestAuthority(r, state)
		if err != nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithAuthority(r, authority)))
	})
}

func contextWithAuthority(r *http.Request, authority admittedAuthority) context.Context {
	return context.WithValue(r.Context(), admissionContextKey{}, authority)
}
func authorityFromRequest(r *http.Request, workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
	authority, ok := r.Context().Value(admissionContextKey{}).(admittedAuthority)
	if !ok {
		return accessidentity.ID{}, errors.New("request has no admitted Access authority")
	}
	return authority(workspace)
}

func resolveRequestAuthority(r *http.Request, state *accessstate.Store) (admittedAuthority, int, error) {
	candidates := recognizedAccessCredentials(r)
	if len(candidates) == 0 {
		return func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil }, 0, nil
	}
	if len(candidates) > 1 {
		return nil, http.StatusUnauthorized, errors.New("ambiguous Access credentials")
	}
	resolution, err := state.Authenticate(candidates[0])
	if err != nil {
		return nil, http.StatusUnauthorized, err
	}
	consumeAccessCredential(r, candidates[0])
	credentialID := resolution.CredentialID
	return func(workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
		current, err := state.ResolveCredential(credentialID)
		if err != nil {
			return accessidentity.ID{}, err
		}
		if current.Workspace != workspace {
			return accessidentity.ID{}, errWorkspaceForbidden
		}
		return current.AccessID, nil
	}, 0, nil
}

var errWorkspaceForbidden = errors.New("credential does not grant the requested Workspace")

func recognizedAccessCredentials(r *http.Request) []string {
	seen := map[string]struct{}{}
	for _, slot := range accessCredentialSlots(r) {
		if strings.HasPrefix(slot.token, "swak_") {
			seen[slot.token] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	return out
}

type accessCredentialLocation uint8

const (
	accessCredentialAuthorization accessCredentialLocation = iota + 1
	accessCredentialHeader
	accessCredentialQuery
)

type accessCredentialSlot struct {
	location accessCredentialLocation
	name     string
	raw      string
	token    string
}

// accessCredentialSlots is the sole parser for managed credential locations.
// Detection and erasure consume the same normalized occurrences, preventing a
// recognized secret from surviving under subtly different parsing semantics.
func accessCredentialSlots(r *http.Request) []accessCredentialSlot {
	var slots []accessCredentialSlot
	for _, raw := range r.Header.Values("Authorization") {
		token := ""
		fields := strings.Fields(raw)
		if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
			token = normalizeAccessCredential(fields[1])
		}
		slots = append(slots, accessCredentialSlot{location: accessCredentialAuthorization, name: "Authorization", raw: raw, token: token})
	}
	for _, name := range []string{"x-api-key", "X-Goog-Api-Key"} {
		for _, raw := range r.Header.Values(name) {
			slots = append(slots, accessCredentialSlot{location: accessCredentialHeader, name: name, raw: raw, token: normalizeAccessCredential(raw)})
		}
	}
	for _, raw := range r.URL.Query()["key"] {
		slots = append(slots, accessCredentialSlot{location: accessCredentialQuery, name: "key", raw: raw, token: normalizeAccessCredential(raw)})
	}
	return slots
}

func normalizeAccessCredential(raw string) string { return strings.TrimSpace(raw) }

func consumeAccessCredential(r *http.Request, token string) {
	slots := accessCredentialSlots(r)
	for _, name := range []string{"Authorization", "x-api-key", "X-Goog-Api-Key"} {
		r.Header.Del(name)
	}
	query := r.URL.Query()
	query.Del("key")
	for _, slot := range slots {
		if slot.token == token {
			continue
		}
		switch slot.location {
		case accessCredentialAuthorization, accessCredentialHeader:
			r.Header.Add(slot.name, slot.raw)
		case accessCredentialQuery:
			query.Add("key", slot.raw)
		}
	}
	r.URL.RawQuery = query.Encode()
	r.RequestURI = r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		r.RequestURI += "?" + r.URL.RawQuery
	}
}

func ValidateLocalAdmission(request *http.Request) error {
	peerHost, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return errLocalAdmissionForbidden
	}
	peer, err := netip.ParseAddr(peerHost)
	if err != nil || !peer.IsLoopback() {
		return errLocalAdmissionForbidden
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	requestAuthority, ok := normalizedLoopbackAuthority(request.Host, scheme)
	if !ok {
		return errLocalAdmissionForbidden
	}
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return nil
	}
	if len(origins) != 1 {
		return errLocalAdmissionForbidden
	}
	rawOrigin := strings.TrimSpace(origins[0])
	if rawOrigin == "" || strings.EqualFold(rawOrigin, "null") {
		return errLocalAdmissionForbidden
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || !strings.EqualFold(origin.Scheme, scheme) || origin.Host == "" || origin.User != nil || origin.Opaque != "" || origin.Path != "" || origin.RawPath != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return errLocalAdmissionForbidden
	}
	originAuthority, ok := normalizedLoopbackAuthority(origin.Host, scheme)
	if !ok || originAuthority != requestAuthority {
		return errLocalAdmissionForbidden
	}
	return nil
}

func normalizedLoopbackAuthority(authority, scheme string) (string, bool) {
	raw := strings.TrimSpace(authority)
	if raw == "" || strings.Contains(raw, "%") {
		return "", false
	}
	parsed, err := url.Parse("//" + raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		host = "localhost"
	} else {
		address, e := netip.ParseAddr(host)
		if e != nil || !address.IsLoopback() {
			return "", false
		}
		host = address.String()
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(scheme, "http") {
			port = "80"
		} else if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			return "", false
		}
	}
	number, e := strconv.Atoi(port)
	if e != nil || number < 1 || number > 65535 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(number)), true
}
