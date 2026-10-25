package accounting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"os"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

var (
	ErrBudgetExceeded = errors.New("workspace estimated spend limit exceeded")
	ErrUnknownPrice   = errors.New("workspace operation price is unavailable")
)

type Mode string

const (
	ModeStopNewBillableWork Mode = "stop_new_billable_work"
	ModeObserveOnly         Mode = "observe_only"
)

type Coverage string

const (
	CoverageKnown   Coverage = "known"
	CoverageUnknown Coverage = "unknown"
)

func (c Coverage) Valid() bool { return c == CoverageKnown || c == CoverageUnknown }

type Limit struct {
	Currency     string `json:"currency"`
	AmountCents  int64  `json:"amount_cents"`
	Window       string `json:"window"`
	Mode         Mode   `json:"mode"`
	UnknownPrice string `json:"unknown_price"`
}

func (limit Limit) Validate() error {
	if limit.AmountCents <= 0 {
		return errors.New("limit amount must be positive; revoke permission to disable access")
	}
	if limit.Currency != "USD" || (limit.Window != "day" && limit.Window != "month") || (limit.Mode != ModeStopNewBillableWork && limit.Mode != ModeObserveOnly) || limit.UnknownPrice != "reject" {
		return errors.New("invalid accounting limit")
	}
	return nil
}

type Reservation struct {
	Workspace   string   `json:"workspace"`
	WindowKey   string   `json:"window_key"`
	Operation   string   `json:"operation"`
	AmountCents int64    `json:"amount_cents"`
	Coverage    Coverage `json:"coverage"`
	State       string   `json:"state"`
	Quote       *Quote   `json:"quote,omitempty"`
}

type SettlementCorrection struct {
	Workspace      string    `json:"workspace"`
	WindowKey      string    `json:"window_key"`
	Operation      string    `json:"operation"`
	PreviousCents  int64     `json:"previous_cents"`
	CorrectedCents int64     `json:"corrected_cents"`
	PreviousState  string    `json:"previous_state"`
	CorrectedState string    `json:"corrected_state"`
	CorrectedAt    time.Time `json:"corrected_at"`
}

type persisted struct {
	Limits       map[string]Limit       `json:"limits"`
	Reservations []Reservation          `json:"reservations"`
	LimitChanges []LimitChange          `json:"limit_changes,omitempty"`
	Corrections  []SettlementCorrection `json:"settlement_corrections,omitempty"`
	WindowFloor  string                 `json:"window_floor,omitempty"`
}

// LimitChange is a credential-free management receipt committed atomically
// with a reviewed limit. Review identity is its idempotency key; facts retain
// the original admitted management actor and exact reviewed intent.
type LimitChange struct {
	ApprovalID string    `json:"approval_id"`
	Workspace  string    `json:"workspace"`
	Actor      string    `json:"actor"`
	Digest     string    `json:"digest"`
	Revision   string    `json:"revision"`
	Before     *Limit    `json:"before,omitempty"`
	After      Limit     `json:"after"`
	AppliedAt  time.Time `json:"applied_at"`
}

type Store struct {
	path            string
	mu              sync.Mutex
	state           persisted
	now             func() time.Time
	policyPublisher func([]byte) error // nil for standalone accounting stores
}

func (s *Store) Revision() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionLocked()
}

func (s *Store) revisionLocked() string {
	var state any = s.state
	if s.policyPublisher != nil {
		state = policyDocument{SchemaVersion: 1, Limits: s.state.Limits, LimitChanges: s.state.LimitChanges}
	}
	raw, _ := json.Marshal(state)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, state: persisted{Limits: map[string]Limit{}}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return nil, fmt.Errorf("decode accounting: %w", err)
	}
	if err := validateState(&s.state); err != nil {
		return nil, err
	}
	return s, nil
}

func validateState(state *persisted) error {
	if state.Limits == nil {
		state.Limits = map[string]Limit{}
	}
	if state.WindowFloor != "" && !validAccountingWindow(state.WindowFloor, "2006-01-02") {
		return errors.New("decode accounting: invalid clock floor")
	}
	for workspace, limit := range state.Limits {
		if err := limit.Validate(); err != nil {
			return fmt.Errorf("decode accounting limit for Workspace %q: %w", workspace, err)
		}
	}
	approvals := make(map[string]struct{}, len(state.LimitChanges))
	for _, change := range state.LimitChanges {
		if change.ApprovalID == "" || change.Workspace == "" || !validLimitActor(change.Actor) || change.Digest == "" || change.Revision == "" || change.AppliedAt.IsZero() {
			return errors.New("decode accounting: incomplete approved limit receipt")
		}
		if _, duplicate := approvals[change.ApprovalID]; duplicate {
			return errors.New("decode accounting: duplicate approved limit receipt")
		}
		approvals[change.ApprovalID] = struct{}{}
		if err := change.After.Validate(); err != nil {
			return errors.New("decode accounting: invalid approved limit outcome")
		}
		if change.Before != nil {
			if err := change.Before.Validate(); err != nil {
				return errors.New("decode accounting: invalid approved limit prior state")
			}
		}
	}
	type reservationIdentity struct{ workspace, window, operation string }
	seen := make(map[reservationIdentity]struct{}, len(state.Reservations))
	for i := range state.Reservations {
		reservation := state.Reservations[i]
		if reservation.Workspace == "" || reservation.Operation == "" || (!validAccountingWindow(reservation.WindowKey, "2006-01-02") && !validAccountingWindow(reservation.WindowKey, "2006-01")) {
			return errors.New("decode accounting: invalid reservation identity")
		}
		identity := reservationIdentity{reservation.Workspace, reservation.WindowKey, reservation.Operation}
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("decode accounting: duplicate reservation identity")
		}
		seen[identity] = struct{}{}
		switch reservation.State {
		case "reserved", "settled", "uncertain", "released":
		default:
			return errors.New("decode accounting: invalid reservation state")
		}
		if reservation.Coverage != "" && !reservation.Coverage.Valid() {
			return errors.New("decode accounting: invalid price coverage")
		}
		if quote := state.Reservations[i].Quote; quote != nil {
			if err := quote.Validate(); err != nil {
				return fmt.Errorf("decode accounting quote: %w", err)
			}
		}
		if state.Reservations[i].AmountCents < 0 {
			return errors.New("decode accounting: reservation amount must not be negative")
		}
		if state.Reservations[i].Coverage == "" {
			state.Reservations[i].Coverage = CoverageUnknown
		}
	}
	return nil
}

func validAccountingWindow(value, layout string) bool {
	parsed, err := time.Parse(layout, value)
	return err == nil && parsed.Format(layout) == value
}

func (s *Store) SetLimit(workspace string, limit Limit) error {
	return s.SetLimitAtRevision(workspace, limit, "")
}

// SetLimitAtRevision applies a limit only when the accounting state still
// matches the revision captured during a trusted preview.
func (s *Store) SetLimitAtRevision(workspace string, limit Limit, expectedRevision string) error {
	return s.setLimit(workspace, limit, expectedRevision, nil, nil)
}

// ApplyReviewedLimitChecked commits the exact change and its management receipt in one
// file replacement. Replaying the same approved operation cannot duplicate it;
// a reused approval with different arguments fails before modifying state.
func (s *Store) ApplyReviewedLimitChecked(ctx context.Context, workspace string, limit Limit, change LimitChange, authorize func(context.Context) error) error {
	if authorize == nil || change.Actor != accessidentity.Attribution(ctx).String() {
		return errors.New("reviewed limit requires original caller admission")
	}
	if change.ApprovalID == "" || change.Workspace != workspace || !validLimitActor(change.Actor) || change.Digest == "" || change.Revision == "" {
		return errors.New("incomplete approved limit receipt")
	}
	return s.setLimit(workspace, limit, change.Revision, &change, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return authorize(ctx)
	})
}

func (s *Store) setLimit(workspace string, limit Limit, expectedRevision string, change *LimitChange, authorize func() error) error {
	if err := limit.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if authorize != nil {
		if err := authorize(); err != nil {
			return err
		}
	}
	if change != nil {
		for _, prior := range s.state.LimitChanges {
			if prior.ApprovalID == change.ApprovalID {
				if prior.Actor == change.Actor && prior.Workspace == workspace && prior.Digest == change.Digest && prior.Revision == change.Revision && prior.After == limit {
					return nil
				}
				return errors.New("approved limit idempotency conflict")
			}
		}
	}
	if expectedRevision != "" {
		if s.revisionLocked() != expectedRevision {
			return errors.New("accounting revision conflict")
		}
	}
	previous, existed := s.state.Limits[workspace]
	s.state.Limits[workspace] = limit
	if change != nil {
		change.After = limit
		change.AppliedAt = s.now().UTC()
		if existed {
			before := previous
			change.Before = &before
		} else {
			change.Before = nil
		}
		s.state.LimitChanges = append(s.state.LimitChanges, *change)
	}
	if err := s.persistPolicy(); err != nil {
		if existed {
			s.state.Limits[workspace] = previous
		} else {
			delete(s.state.Limits, workspace)
		}
		if change != nil {
			s.state.LimitChanges = s.state.LimitChanges[:len(s.state.LimitChanges)-1]
		}
		return err
	}
	return nil
}

// ClearLimitAtRevision removes desired spending policy without touching in-flight
// reservations or historical pricing evidence. Recreating a limit later must
// still account for all remaining reservations in its economic window.
func (s *Store) ClearLimitAtRevision(workspace, expectedRevision string) error {
	return s.clearLimit(workspace, expectedRevision, nil)
}

// ClearLimitChecked repeats caller admission inside the policy publication lock.
func (s *Store) ClearLimitChecked(ctx context.Context, workspace, expected string, authorize func(context.Context) error) error {
	if expected == "" || authorize == nil {
		return errors.New("limit clear requires revision and admission")
	}
	return s.clearLimit(workspace, expected, func() error { return authorize(ctx) })
}

func (s *Store) clearLimit(workspace, expectedRevision string, authorize func() error) error {
	if workspace == "" {
		return errors.New("Workspace is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if authorize != nil {
		if err := authorize(); err != nil {
			return err
		}
	}
	if expectedRevision != "" && expectedRevision != s.revisionLocked() {
		return errors.New("accounting revision conflict")
	}
	before, exists := s.state.Limits[workspace]
	if !exists {
		return nil
	}
	delete(s.state.Limits, workspace)
	if err := s.persistPolicy(); err != nil {
		s.state.Limits[workspace] = before
		return err
	}
	return nil
}

// LimitChanges returns an independent local Operator audit projection. It must
// never be included in model diagnostics or ordinary Workspace data responses.
func (s *Store) LimitChanges(workspace string) []LimitChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limitChangesLocked(workspace, false)
}

// AllLimitChanges returns detached retained management facts across Workspaces
// for trusted Node inspection, never model diagnostics or current limit truth.
func (s *Store) AllLimitChanges() []LimitChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limitChangesLocked("", true)
}

func (s *Store) limitChangesLocked(workspace string, all bool) []LimitChange {
	var out []LimitChange
	for _, change := range s.state.LimitChanges {
		if !all && change.Workspace != workspace {
			continue
		}
		if change.Before != nil {
			before := *change.Before
			change.Before = &before
		}
		out = append(out, change)
	}
	return out
}

func (s *Store) Limit(workspace string) (Limit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Limits[workspace]
	return value, ok
}

func (s *Store) Reserve(workspace, operation string, amountCents int64, knownPrice bool) (Reservation, error) {
	return s.reserve(workspace, operation, amountCents, knownPrice, nil)
}

func (s *Store) ReserveQuote(workspace, operation string, quote Quote) (Reservation, error) {
	if err := quote.Validate(); err != nil {
		return Reservation{}, err
	}
	if workspace == "" || operation == "" {
		return Reservation{}, errors.New("accounting quote reservation identity is incomplete")
	}
	return s.reserve(workspace, operation, quote.AmountCents, true, cloneQuote(&quote))
}

func (s *Store) reserve(workspace, operation string, amountCents int64, knownPrice bool, quote *Quote) (Reservation, error) {
	if workspace == "" || operation == "" {
		return Reservation{}, errors.New("accounting reservation identity is incomplete")
	}
	if amountCents < 0 {
		return Reservation{}, errors.New("reservation amount must not be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	limit, limited := s.state.Limits[workspace]
	if !knownPrice && limited && limit.Mode == ModeStopNewBillableWork && limit.UnknownPrice == "reject" {
		return Reservation{}, ErrUnknownPrice
	}
	window, err := s.accountingWindow(limit.Window)
	if err != nil {
		return Reservation{}, err
	}
	for _, reservation := range s.state.Reservations {
		if reservation.Workspace == workspace && reservation.WindowKey == window && reservation.Operation == operation {
			reservation.Quote = cloneQuote(reservation.Quote)
			return reservation, nil
		}
	}
	if limited && limit.Mode == ModeStopNewBillableWork {
		remaining := limit.AmountCents
		for _, reservation := range s.state.Reservations {
			if reservation.Workspace == workspace && reservation.WindowKey == window && reservation.State != "released" {
				if reservation.AmountCents > remaining {
					return Reservation{}, ErrBudgetExceeded
				}
				remaining -= reservation.AmountCents
			}
		}
		if amountCents > remaining {
			return Reservation{}, ErrBudgetExceeded
		}
	}
	coverage := CoverageKnown
	if !knownPrice {
		coverage = CoverageUnknown
	}
	reservation := Reservation{Workspace: workspace, WindowKey: window, Operation: operation, AmountCents: amountCents, Coverage: coverage, State: "reserved", Quote: quote}
	s.state.Reservations = append(s.state.Reservations, reservation)
	if err := s.persist(); err != nil {
		s.state.Reservations = s.state.Reservations[:len(s.state.Reservations)-1]
		return Reservation{}, err
	}
	reservation.Quote = cloneQuote(reservation.Quote)
	return reservation, nil
}

func (s *Store) Settle(workspace, operation string, actualCents int64, uncertain bool) error {
	if actualCents < 0 {
		return errors.New("settlement amount must not be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	limit, _ := s.state.Limits[workspace]
	window, err := s.accountingWindow(limit.Window)
	if err != nil {
		return err
	}
	return s.settleLocked(workspace, window, operation, actualCents, uncertain)
}

// SettleReservation settles the exact reservation returned by Reserve. The
// explicit window permits an in-flight operation to cross midnight without
// mutating a different period or being stranded by the current clock window.
func (s *Store) SettleReservation(workspace, window, operation string, actualCents int64, uncertain bool) error {
	if actualCents < 0 {
		return errors.New("settlement amount must not be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if workspace == "" || window == "" || operation == "" {
		return errors.New("accounting reservation identity is incomplete")
	}
	return s.settleLocked(workspace, window, operation, actualCents, uncertain)
}

func (s *Store) settleLocked(workspace, window, operation string, actualCents int64, uncertain bool) error {
	match := -1
	for i := range s.state.Reservations {
		if s.state.Reservations[i].Workspace == workspace && s.state.Reservations[i].Operation == operation && s.state.Reservations[i].WindowKey == window {
			match = i
			break
		}
	}
	if match >= 0 {
		i := match
		state := "settled"
		if uncertain {
			state = "uncertain"
		}
		current := s.state.Reservations[i]
		if current.AmountCents == actualCents && current.State == state {
			return nil
		}
		correctionCount := len(s.state.Corrections)
		if current.State != "reserved" {
			s.state.Corrections = append(s.state.Corrections, SettlementCorrection{
				Workspace: workspace, WindowKey: window, Operation: operation,
				PreviousCents: current.AmountCents, CorrectedCents: actualCents,
				PreviousState: current.State, CorrectedState: state, CorrectedAt: s.now().UTC(),
			})
		}
		s.state.Reservations[i].AmountCents = actualCents
		s.state.Reservations[i].State = state
		if err := s.persist(); err != nil {
			s.state.Reservations[i] = current
			s.state.Corrections = s.state.Corrections[:correctionCount]
			return err
		}
		return nil
	}
	return errors.New("accounting reservation not found")
}

func (s *Store) accountingWindow(window string) (string, error) {
	observedDay := s.now().UTC().Format("2006-01-02")
	if s.state.WindowFloor == "" || observedDay > s.state.WindowFloor {
		previous := s.state.WindowFloor
		s.state.WindowFloor = observedDay
		if err := s.persist(); err != nil {
			s.state.WindowFloor = previous
			return "", err
		}
	}
	if window == "month" {
		return s.state.WindowFloor[:len("2006-01")], nil
	}
	return s.state.WindowFloor, nil
}

func (s *Store) SettlementCorrections(workspace string) []SettlementCorrection {
	s.mu.Lock()
	defer s.mu.Unlock()
	corrections := make([]SettlementCorrection, 0)
	for _, correction := range s.state.Corrections {
		if correction.Workspace == workspace {
			corrections = append(corrections, correction)
		}
	}
	return corrections
}

// Reservations returns a copied local projection of Workspace reservations.
// Coverage remains explicit so callers cannot interpret an unknown price as a
// known zero-cost reservation.
func (s *Store) Reservations(workspace string) []Reservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	reservations := make([]Reservation, 0)
	for _, reservation := range s.state.Reservations {
		if reservation.Workspace == workspace {
			reservation.Quote = cloneQuote(reservation.Quote)
			reservations = append(reservations, reservation)
		}
	}
	return reservations
}

func (s *Store) persistPolicy() error {
	if s.policyPublisher == nil {
		return s.persist()
	}
	raw, err := json.Marshal(policyDocument{SchemaVersion: 1, Limits: s.state.Limits, LimitChanges: s.state.LimitChanges})
	if err != nil {
		return err
	}
	return s.policyPublisher(raw)
}

func (s *Store) persist() error {
	var state any = s.state
	if s.policyPublisher != nil {
		state = runtimeDocument{Reservations: s.state.Reservations, Corrections: s.state.Corrections, WindowFloor: s.state.WindowFloor}
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Replace(s.path, append(raw, '\n'), 0o600)
}

func validLimitActor(raw string) bool { _, err := accessidentity.Parse(raw); return err == nil }
