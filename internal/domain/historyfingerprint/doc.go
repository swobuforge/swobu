// Package historyfingerprint owns opaque, protocol-scheme fingerprints for
// completed client-visible history and its ordered request/response leaves.
//
// Client codecs choose a private scheme and provide protocol-native material.
// Codecs may fold supplied completed history; exchange alone composes a newly
// completed exchange. This package knows no canonical, wire, session,
// provider, storage, or transport values.
package historyfingerprint
