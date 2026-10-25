package connectioncheck

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// CredentialResolver resolves the inspected Connection's locator. Provider
// discovery may resolve repeatedly, but cannot choose another owner or binding.
type CredentialResolver func(context.Context, credentialref.Scope, string) (string, error)

type modelCheckCredentials struct {
	scope       credentialref.Scope
	ref         string
	resolve     CredentialResolver
	unavailable bool
}

func (c *modelCheckCredentials) ResolveCredential(ctx context.Context, _ string, ref string) (string, error) {
	if ref != c.ref || c.resolve == nil {
		c.unavailable = true
		return "", errCheckCredential
	}
	token, err := c.resolve(ctx, c.scope, ref)
	if err != nil || strings.TrimSpace(token) == "" {
		c.unavailable = true
		return "", errCheckCredential
	}
	return token, nil
}

// ResolveScopedCredential admits only the inspected owner and locator. Provider
// discovery cannot substitute another Connection even when it shares a token.
func (c *modelCheckCredentials) ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, ref string) (string, error) {
	if scope != c.scope {
		c.unavailable = true
		return "", errCheckCredential
	}
	return c.ResolveCredential(ctx, "", ref)
}

var errCheckCredential = errors.New("Connection credential unavailable")

// Models probes the existing provider catalog, never inference. Empty/advisory
// success alone does not establish capability: transport evidence must include
// successful HTTP and provider decoding must produce advertised model options.
// An empty decoded catalog remains reachable with unknown Models capability.
func Models(ctx context.Context, value domain.Connection, resolve CredentialResolver) (domain.Check, error) {
	transport := newModelTransport()
	defer transport.CloseIdleConnections()
	return checkModels(ctx, value, resolve, transport)
}

func checkModels(ctx context.Context, value domain.Connection, resolve CredentialResolver, transport http.RoundTripper) (domain.Check, error) {
	spec, ok := value.Spec().(domain.ModelSpec)
	if !ok {
		return domain.Check{}, errors.New("Models Connection required")
	}
	observe := func(reach domain.Reachability, auth domain.Authentication, capabilities []domain.Capability, failure domain.CheckError) (domain.Check, error) {
		return domain.NewCheck(value, time.Now().UTC(), reach, auth, capabilities, failure)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	providerID := string(spec.Upstream().Provider())
	protocols := profile.ConcreteProviderProtocolsForSpec(providerID)
	if len(protocols) == 0 {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckUnavailable)
	}
	target, err := exchange.ProviderTargetFromModelConnection(value.ID().String(), value, protocols[0])
	if err != nil {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckUnavailable)
	}
	scope, err := target.ConnectionCredentialScope()
	if err != nil {
		return domain.Check{}, err
	}
	credentials := &modelCheckCredentials{scope: scope, ref: target.CredentialRef, resolve: resolve}
	if target.CredentialRef != "" {
		if _, err := credentials.ResolveCredential(ctx, providerID, target.CredentialRef); err != nil {
			return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckCredentialUnavailable)
		}
	}
	evidence := &modelResponseTransport{next: transport, pinOrigin: providerID != string(profile.ProviderSpecBedrock) || target.CredentialRef != ""}
	client := modelCheckClient(evidence)
	registry, err := providers.NewProviderRegistry(client, credentials)
	if err != nil {
		return domain.Check{}, err
	}
	var probe provider.TargetProbeResult
	if providerID == string(profile.ProviderSpecBedrock) {
		// The normal Bedrock probe adds STS enrichment. Only its catalog-only
		// seam belongs here, including bounded credential-chain HTTP effects.
		probe, err = bedrock.CheckConnectionCatalog(ctx, target, client, credentials)
	} else if providerID == string(profile.ProviderSpecChatGPT) {
		probe, err = chatgpt.CheckConnectionCatalog(ctx, target, client, credentials)
	} else {
		probe, err = registry.ProbeTarget(ctx, target)
	}
	return modelObservation(observe, evidence, credentials, ctx, probe.Options, err, target.CredentialRef != "")
}

type observer func(domain.Reachability, domain.Authentication, []domain.Capability, domain.CheckError) (domain.Check, error)

func modelObservation(observe observer, evidence *modelResponseTransport, credentials *modelCheckCredentials, ctx context.Context, options []profile.ModelAuthoringOption, err error, explicitCredential bool) (domain.Check, error) {
	if credentials.unavailable {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckCredentialUnavailable)
	}
	if evidence.authRejected {
		return observe(domain.Reachable, domain.AuthenticationRejected, nil, domain.CheckAuthenticationFailed)
	}
	if evidence.denied {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckNetworkDenied)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckTimeout)
	}
	if evidence.received == 0 {
		return observe(domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckUnavailable)
	}
	if err != nil || evidence.failed {
		failure := domain.CheckUnavailable
		if evidence.invalid || evidence.successful > 0 && !evidence.badStatus {
			failure = domain.CheckInvalidResponse
		}
		return observe(domain.Reachable, domain.AuthenticationUnknown, nil, failure)
	}
	if evidence.successful == 0 {
		return observe(domain.Reachable, domain.AuthenticationUnknown, nil, domain.CheckUnavailable)
	}
	auth := domain.AuthenticationUnknown
	if explicitCredential {
		auth = domain.AuthenticationAccepted
	}
	var capabilities []domain.Capability
	if len(options) > 0 {
		capabilities = []domain.Capability{domain.CapabilityModels}
	}
	return observe(domain.Reachable, auth, capabilities, domain.CheckOK)
}
