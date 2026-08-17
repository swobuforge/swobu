// Package historyfingerprint owns opaque, protocol-scheme fingerprints for
// completed client-visible history and its ordered request/response leaves.
//
// Fingerprint material contains only protocol-native values whose identity
// survives as completed client-visible history across invocations.
// Request-scoped controls, execution metadata, credentials, delivery settings,
// and other invocation-local facts are excluded even when nested inside an
// otherwise historical wire object.
//
// Client codecs choose a private scheme and provide protocol-native material.
// Codecs may fold supplied completed history; exchange alone composes a newly
// completed exchange. This package knows no canonical, wire, session,
// provider, storage, or transport values.
package historyfingerprint
