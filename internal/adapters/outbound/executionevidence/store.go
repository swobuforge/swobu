// Package executionevidence provides the bounded local lifecycle store for
// immutable execution evidence. It groups facts by exchange rather than
// evicting individual events, so active lifecycles are never orphaned.
// Direct MCP operations have independent lifecycle identity and retain active
// calls separately from bounded settled history; they do not fabricate model
// traffic. Activity projections are local memory evidence, not durable Ledger.
// Optional OfferMCP hands immutable completed facts to a non-blocking output
// consumer after releasing the store lock. Capture rejection cannot rewrite
// settlement; the consumer owns truthful capture-gap health and shutdown.
// OfferExchange follows the same post-publication, outside-lock handoff for
// immutable completed Exchange lifecycles; it does not invent parent Exchanges
// for standalone operations.
// Activity can merge immutable completed Exchange history without inserting it
// into live storage or recapturing it. Exact live identity wins overlap; durable
// and live rows reuse the same model-attempt and usage projection.
package executionevidence

import (
	"errors"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
)

const (
	defaultSettledLimit   = 512
	defaultAdmissionLimit = 256
)

type Config struct {
	SettledLimit   int
	AdmissionLimit int
	OfferMCP       func(domain.CompletedMCPOperation) bool
	OfferExchange  func(domain.CompletedExchange) bool
}

type InvocationLifecycle struct {
	Started domain.InvocationStartedEvent
	Settled domain.InvocationSettledEvent
}

type ExchangeLifecycle struct {
	Started     domain.ExchangeStarted
	Invocations []InvocationLifecycle
	Settled     *domain.ExchangeSettled
}

type Store struct {
	mu sync.RWMutex

	settledLimit   int
	admissionLimit int
	active         map[execution.ExchangeID]*ExchangeLifecycle
	settled        map[execution.ExchangeID]ExchangeLifecycle
	settledOrder   []execution.ExchangeID
	admissions     []domain.ManagedAccessAdmissionRejected
	mcpActive      map[string]MCPOperation
	mcpSettled     []MCPOperation
	offerMCP       func(domain.CompletedMCPOperation) bool
	offerExchange  func(domain.CompletedExchange) bool
}

func NewStore(cfg Config) *Store {
	if cfg.SettledLimit <= 0 {
		cfg.SettledLimit = defaultSettledLimit
	}
	if cfg.AdmissionLimit <= 0 {
		cfg.AdmissionLimit = defaultAdmissionLimit
	}
	return &Store{
		settledLimit: cfg.SettledLimit, admissionLimit: cfg.AdmissionLimit,
		offerMCP:      cfg.OfferMCP,
		offerExchange: cfg.OfferExchange,
		active:        make(map[execution.ExchangeID]*ExchangeLifecycle), settled: make(map[execution.ExchangeID]ExchangeLifecycle),
	}
}

// Append records one lifecycle fact. Invalid transitions are programming
// errors returned to the caller; the store never silently repairs them.
func (s *Store) Append(event domain.Event) error {
	if s == nil {
		return errors.New("nil execution evidence store")
	}
	s.mu.Lock()
	var captured *domain.CompletedExchange
	defer func() {
		s.mu.Unlock()
		if captured != nil && s.offerExchange != nil {
			s.offerExchange(*captured)
		}
	}()
	switch e := event.(type) {
	case domain.ManagedAccessAdmissionRejected:
		s.admissions = append(s.admissions, e)
		if len(s.admissions) > s.admissionLimit {
			s.admissions = append([]domain.ManagedAccessAdmissionRejected(nil), s.admissions[len(s.admissions)-s.admissionLimit:]...)
		}
		return nil
	case domain.ExchangeStarted:
		key := e.ID()
		if _, ok := s.active[key]; ok {
			return errors.New("exchange already started")
		}
		if _, ok := s.settled[key]; ok {
			return errors.New("exchange identity was already settled")
		}
		s.active[key] = &ExchangeLifecycle{Started: e}
		return nil
	case domain.ModelInvocationStarted:
		return s.appendInvocationStartedLocked(e)
	case domain.MCPInvocationStarted:
		return s.appendInvocationStartedLocked(e)
	case domain.ModelInvocationSettled:
		return s.appendInvocationSettledLocked(e)
	case domain.MCPInvocationSettled:
		return s.appendInvocationSettledLocked(e)
	case domain.ExchangeSettled:
		key := e.ID()
		record, ok := s.active[key]
		if !ok {
			return errors.New("exchange settlement requires a started exchange")
		}
		if record.Settled != nil {
			return errors.New("exchange already settled")
		}
		for _, invocation := range record.Invocations {
			if invocation.Settled == nil {
				return errors.New("exchange settlement requires every invocation to settle")
			}
		}
		if s.offerExchange != nil {
			invocations := make([]domain.InvocationLifecycle, 0, len(record.Invocations))
			for _, invocation := range record.Invocations {
				lifecycle, err := domain.NewInvocationLifecycle(invocation.Started, invocation.Settled)
				if err != nil {
					return err
				}
				invocations = append(invocations, lifecycle)
			}
			completed, err := domain.NewCompletedExchange(record.Started, invocations, e)
			if err != nil {
				return err
			}
			captured = &completed
		}
		record.Settled = &e
		complete := cloneLifecycle(*record)
		delete(s.active, key)
		s.settled[key] = complete
		s.settledOrder = append(s.settledOrder, key)
		for len(s.settledOrder) > s.settledLimit {
			oldest := s.settledOrder[0]
			s.settledOrder = s.settledOrder[1:]
			delete(s.settled, oldest)
		}
		return nil
	default:
		return errors.New("unsupported execution evidence event")
	}
}

func (s *Store) appendInvocationSettledLocked(e domain.InvocationSettledEvent) error {
	_, invocation, ok := s.findInvocationLocked(e.ID())
	if !ok {
		return errors.New("invocation settlement requires a started invocation")
	}
	if invocation.Settled != nil {
		return errors.New("invocation already settled")
	}
	invocation.Settled = e
	return nil
}

func (s *Store) appendInvocationStartedLocked(e domain.InvocationStartedEvent) error {
	key := e.ExchangeID()
	record, ok := s.active[key]
	if !ok {
		return errors.New("invocation requires an active exchange")
	}
	if e.Ordinal() != uint64(len(record.Invocations)+1) {
		return errors.New("invocation ordinal is not the next exchange ordinal")
	}
	for _, invocation := range record.Invocations {
		if invocation.Started.ID() == e.ID() {
			return errors.New("invocation already started")
		}
	}
	record.Invocations = append(record.Invocations, InvocationLifecycle{Started: e})
	return nil
}

// AppendExecutionEvidence implements observation.ExecutionEvidenceSink.
func (s *Store) AppendExecutionEvidence(event domain.Event) error { return s.Append(event) }

func (s *Store) findInvocationLocked(id execution.InvocationID) (*ExchangeLifecycle, *InvocationLifecycle, bool) {
	record, ok := s.active[id.ExchangeID()]
	if !ok {
		return nil, nil, false
	}
	ordinal := id.Ordinal()
	if ordinal == 0 || ordinal > uint64(len(record.Invocations)) {
		return nil, nil, false
	}
	invocation := &record.Invocations[ordinal-1]
	if invocation.Started.ID() == id {
		return record, invocation, true
	}
	return nil, nil, false
}

// Active returns complete shallow-cloned active lifecycles.
func (s *Store) Active() []ExchangeLifecycle {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ExchangeLifecycle, 0, len(s.active))
	for _, record := range s.active {
		out = append(out, cloneLifecycle(*record))
	}
	return out
}

// Settled returns complete lifecycles in oldest-to-newest retention order.
func (s *Store) Settled() []ExchangeLifecycle {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ExchangeLifecycle, 0, len(s.settledOrder))
	for _, key := range s.settledOrder {
		out = append(out, cloneLifecycle(s.settled[key]))
	}
	return out
}

func (s *Store) Admissions() []domain.ManagedAccessAdmissionRejected {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.ManagedAccessAdmissionRejected(nil), s.admissions...)
}

// Exchange returns detached live/settled lifecycle facts for one exact identity.
// Absence only describes this bounded process-local store, not durable history.
func (s *Store) Exchange(identity execution.ExchangeID) (ExchangeLifecycle, bool) {
	if s == nil || !identity.Valid() {
		return ExchangeLifecycle{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if record, found := s.active[identity]; found {
		return cloneLifecycle(*record), true
	}
	record, found := s.settled[identity]
	if !found {
		return ExchangeLifecycle{}, false
	}
	if record.Settled != nil {
		settled := *record.Settled
		record.Settled = &settled
	}
	return cloneLifecycle(record), true
}

func cloneLifecycle(record ExchangeLifecycle) ExchangeLifecycle {
	record.Invocations = append([]InvocationLifecycle(nil), record.Invocations...)
	return record
}
