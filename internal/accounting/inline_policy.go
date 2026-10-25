package accounting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// policyDocument is Node desired configuration. Approved command receipts remain
// alongside policy until the audit/approval lifecycle is replaced; keeping them
// with the limit ensures crash-safe idempotency without a cross-file transaction.
// Runtime quotes and unsettled provider effects must NEVER be serialized here.
type policyDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Limits        map[string]Limit `json:"limits"`
	LimitChanges  []LimitChange    `json:"limit_changes,omitempty"`
}

// runtimeDocument is durable execution evidence. It has no authority to set
// spending policy, even if an old file once carried such fields.
type runtimeDocument struct {
	Reservations []Reservation          `json:"reservations,omitempty"`
	Corrections  []SettlementCorrection `json:"settlement_corrections,omitempty"`
	WindowFloor  string                 `json:"window_floor,omitempty"`
}

// ValidatePolicyDocument shares reopening validation with the live store and
// permits absent policy only for a Node with no configured budget limits.
func ValidatePolicyDocument(raw []byte) (map[string]Limit, error) {
	state, err := decodePolicy(raw)
	if err != nil {
		return nil, err
	}
	return state.Limits, nil
}

func decodePolicy(raw []byte) (persisted, error) {
	state := persisted{Limits: map[string]Limit{}}
	if len(raw) == 0 {
		return state, nil
	}
	var policy policyDocument
	if err := strictPolicyJSON(raw, &policy); err != nil {
		return state, fmt.Errorf("accounting policy: %w", err)
	}
	if policy.SchemaVersion != 1 || policy.Limits == nil {
		return state, errors.New("accounting policy: expected schema_version 1 and limits mapping")
	}
	state.Limits, state.LimitChanges = policy.Limits, policy.LimitChanges
	if err := validateState(&state); err != nil {
		return state, err
	}
	return state, nil
}

func strictPolicyJSON(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("more than one document")
		}
		return err
	}
	return nil
}

// OpenPolicyInline is the production accounting owner. It takes limit policy
// from the already-locked Node file; accounting.json is ONLY a durable runtime
// journal of reservations, price snapshots, corrections and the clock floor.
// Obsolete sidecars containing policy fail closed rather than silently winning.
func OpenPolicyInline(runtimePath string, policyRaw []byte, publish func([]byte) error) (*Store, error) {
	if publish == nil {
		return nil, errors.New("accounting policy publisher is required")
	}
	policy, err := decodePolicy(policyRaw)
	if err != nil {
		return nil, err
	}
	runtimeRaw, err := os.ReadFile(runtimePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		var fields map[string]json.RawMessage
		if err := strictPolicyJSON(runtimeRaw, &fields); err != nil {
			return nil, fmt.Errorf("decode accounting runtime: %w", err)
		}
		if _, found := fields["limits"]; found {
			return nil, errors.New("obsolete accounting.json contains limits; move desired policy to swobu.yaml explicitly")
		}
		if _, found := fields["limit_changes"]; found {
			return nil, errors.New("obsolete accounting.json contains approved limit receipts; move them to swobu.yaml explicitly")
		}
		for field := range fields {
			if field != "reservations" && field != "settlement_corrections" && field != "window_floor" {
				return nil, fmt.Errorf("accounting runtime: unsupported field %q", field)
			}
		}
	}
	// Decode exactly the bytes that passed the obsolete-authority check.
	// Reading the journal a second time would permit an intervening replacement
	// to smuggle policy fields back into an ostensibly runtime-only sidecar.
	store := &Store{path: runtimePath, now: time.Now, state: persisted{Limits: map[string]Limit{}}, policyPublisher: publish}
	if runtimeRaw != nil {
		if decodeErr := json.Unmarshal(runtimeRaw, &store.state); decodeErr != nil {
			return nil, fmt.Errorf("decode accounting runtime: %w", decodeErr)
		}
		if validateErr := validateState(&store.state); validateErr != nil {
			return nil, validateErr
		}
	}
	store.state.Limits = policy.Limits
	store.state.LimitChanges = policy.LimitChanges
	return store, nil
}

// ValidateRuntimeJournal performs a read-only integrity and obsolete-authority
// check. An absent journal is valid; policy-only changes must not create one.
func ValidateRuntimeJournal(path string) error {
	_, err := OpenPolicyInline(path, nil, func([]byte) error { return errors.New("read-only validator") })
	return err
}
