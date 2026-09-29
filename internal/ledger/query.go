package ledger

import (
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

const (
	DefaultPageSize = 50
	MaximumPageSize = 200
)

// TimeRange is an inclusive lower and exclusive upper wall-time range. A zero
// endpoint is open; when both are present the end must be after the start.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

func (r TimeRange) Validate() error {
	if !r.Start.IsZero() && !r.End.IsZero() && !r.End.After(r.Start) {
		return errors.New("time range end must be after start")
	}
	return nil
}

type InvocationKind string

const (
	InvocationModel InvocationKind = "model"
	InvocationMCP   InvocationKind = "mcp_tool"
)

func (k InvocationKind) Valid() bool { return k == InvocationModel || k == InvocationMCP }

// ExchangeFilter is the closed portable history filter. All present fields are
// ANDed. Implementations must apply all Invocation fields to one same
// Invocation rather than satisfying them across different Invocations.
type ExchangeFilter struct {
	Settled           TimeRange
	Workspace         *routing.WorkspaceSlug
	Access            *accessidentity.ID
	Route             *routing.RouteName
	ClientFamily      *clientfamily.Family
	ClientProtocol    *protocolkind.ProtocolKind
	DeliveryResult    *delivery.ResultKind
	CanonicalError    *canonical.ErrorCode
	Provider          *routing.Provider
	ProviderModel     *routing.UpstreamModel
	Target            *routing.TargetID
	InvocationKind    *InvocationKind
	InvocationOutcome *execution.Outcome
	InvocationState   *execution.Occurrence
	ModelFailure      *executionevidence.ModelFailure
	MCPFailure        *executionevidence.MCPFailure
}

func (f ExchangeFilter) Validate() error {
	if err := f.Settled.Validate(); err != nil {
		return err
	}
	if f.Workspace != nil && f.Workspace.String() == "" || f.Access != nil && !f.Access.Valid() || f.Route != nil && f.Route.String() == "" || f.ClientFamily != nil && !f.ClientFamily.Valid() {
		return errors.New("exchange filter contains an invalid identity")
	}
	if f.ClientProtocol != nil {
		if _, err := protocolkind.ParseProtocolKind(f.ClientProtocol.String()); err != nil {
			return err
		}
	}
	if f.DeliveryResult != nil && !delivery.ValidResultKind(*f.DeliveryResult) || f.CanonicalError != nil && !canonical.ValidErrorCode(*f.CanonicalError) || f.Provider != nil && *f.Provider == "" || f.ProviderModel != nil && f.ProviderModel.String() == "" || f.Target != nil && f.Target.String() == "" {
		return errors.New("exchange filter contains an invalid closed value")
	}
	if f.InvocationKind != nil && !f.InvocationKind.Valid() || f.InvocationOutcome != nil && !f.InvocationOutcome.Valid() || f.InvocationState != nil && !f.InvocationState.Valid() || f.ModelFailure != nil && !f.ModelFailure.Valid() || f.MCPFailure != nil && !f.MCPFailure.Valid() {
		return errors.New("exchange filter contains an invalid Invocation value")
	}
	if f.ModelFailure != nil && f.MCPFailure != nil {
		return errors.New("exchange filter cannot require model and MCP failure classes together")
	}
	if f.InvocationKind != nil && ((*f.InvocationKind == InvocationModel && f.MCPFailure != nil) || (*f.InvocationKind == InvocationMCP && f.ModelFailure != nil)) {
		return errors.New("exchange filter Invocation kind and failure class disagree")
	}
	if f.ModelFailure != nil && f.InvocationKind != nil && *f.InvocationKind != InvocationModel {
		return errors.New("model failure requires model invocation kind")
	}
	if f.MCPFailure != nil && f.InvocationKind != nil && *f.InvocationKind != InvocationMCP {
		return errors.New("MCP failure requires MCP invocation kind")
	}
	return nil
}

// Cursor identifies the last item of a descending `(settled_at, exchange_id)`
// page. It deliberately has no public wire encoding in M5.
type Cursor struct {
	settledAt time.Time
	exchange  execution.ExchangeID
}

func NewCursor(settledAt time.Time, exchange execution.ExchangeID) (Cursor, error) {
	if settledAt.IsZero() || !exchange.Valid() {
		return Cursor{}, errors.New("Ledger cursor requires settlement time and Exchange identity")
	}
	return Cursor{settledAt: settledAt, exchange: exchange}, nil
}

func (c Cursor) Valid() bool                      { return !c.settledAt.IsZero() && c.exchange.Valid() }
func (c Cursor) SettledAt() time.Time             { return c.settledAt }
func (c Cursor) ExchangeID() execution.ExchangeID { return c.exchange }

// ExchangeSummary is the bounded activity row. Invocation detail is loaded
// only by GetExchange, so list pages do not reconstruct every lifecycle.
type ExchangeSummary struct {
	ID             execution.ExchangeID
	Access         accessidentity.ID
	Workspace      routing.WorkspaceSlug
	StartedAt      time.Time
	SettledAt      time.Time
	Duration       time.Duration
	ClientFamily   clientfamily.Family
	ClientProtocol protocolkind.ProtocolKind
	Operation      canonical.NormalizedPath
	Result         delivery.ResultKind
	Status         int
	Error          canonical.ErrorCode
}

type ExchangeSummaryPage struct {
	Exchanges []ExchangeSummary
	Next      Cursor
}

func NormalizePageSize(size int) (int, error) {
	if size == 0 {
		return DefaultPageSize, nil
	}
	if size < 0 || size > MaximumPageSize {
		return 0, errors.New("Ledger page size must be between 1 and 200")
	}
	return size, nil
}
