package accessidentity

import "context"

type attributionKey struct{}

// WithAttribution carries an already admitted Principal to retained management
// facts. It never authorizes a command; the owning command still calls its
// separate live admission callback. Transport arguments cannot set this value.
func WithAttribution(ctx context.Context, caller ID) context.Context {
	return context.WithValue(ctx, attributionKey{}, caller)
}

// Attribution defaults to the established trusted-local command attribution.
// Remote adapters must supply their admitted Principal explicitly.
func Attribution(ctx context.Context) ID {
	if caller, ok := ctx.Value(attributionKey{}).(ID); ok && caller.Valid() {
		return caller
	}
	return Local
}
