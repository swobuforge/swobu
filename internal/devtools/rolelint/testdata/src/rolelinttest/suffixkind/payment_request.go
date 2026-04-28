package suffixkind

type PaymentRequest struct{} // want `data-suffix struct "PaymentRequest" must not declare operational methods; use behavior-owner naming or move behavior out; operational methods found: Execute`

func (PaymentRequest) Execute() {}

type RouteSelector struct{} // want `behavior-suffix struct "RouteSelector" must declare at least one method`

type PlainObject struct{} // want `no-method struct "PlainObject" must use a data suffix`

type TraceSpec struct{}

type ValueRequest struct{}

func (ValueRequest) Model() string       { return "model" }
func (ValueRequest) HasThread() bool     { return false }
func (ValueRequest) Clone() ValueRequest { return ValueRequest{} }

type TargetResolver struct{}

func (TargetResolver) Resolve() string { return "ok" }
