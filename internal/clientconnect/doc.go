// Package clientconnect discovers and surgically rewires supported local AI
// clients to one canonical, unversioned workspace URL. Cockpit discovery and
// the headless connect command are projections of the same Plan and Apply.
//
// Foreign client configuration remains its own source of truth. A static
// registry owns only catalogue ordering and lookup. Each adapter owns its
// identity, name, presence signal, paths, admission, and planning semantics;
// when a client field selects network/backend transport, the adapter declares
// Swobu there and selects the facade model `default`. A client-internal profile
// discriminator may retain a required provider value only when an independent
// endpoint field unambiguously selects Swobu and the discriminator cannot
// redirect traffic; Muse's `provider=meta` and `endpoint_transport.base_url`
// are the bounded example. Endpoint-only mutation is valid only for an explicit gateway/proxy seam such
// as Claude Code's ANTHROPIC_BASE_URL; the Claude adapter also enables the
// client's gateway model-discovery request. Target provider/model and capability
// metadata derived from the selected target remain encapsulated behind the
// workspace route. A client-facing capability may be declared only when Swobu
// itself intentionally guarantees it as part of the facade contract, such as
// Kilo's tool-call support.
// shared JSON-family and TOML editors own only source-preserving string-path
// mechanics. An adapter may instead delegate persistence to a documented
// client config command through Service's private no-shell argv runner. Apply
// resolves the current adapter from the registry, re-plans current client
// semantics, compares the reviewed semantic changes, and invokes only that
// freshly resolved mutation. Ordered writes are permitted only when every
// committed prefix is harmless; otherwise the adapter performs one atomic
// file replacement. Deliberate-open discovery isolates each adapter's cheap
// presence and Plan failures. The package stores no binding, receipt,
// credential, model, or capability metadata.
package clientconnect
