// Package responsesnative owns the validated opaque transcript required for
// exact stateless replay through the Responses protocol.
//
// Canonical may contain typed protocol-native refinements when they are
// attached beneath a canonical semantic owner. This package owns the other
// case: an independent protocol replay transcript carried beside canonical
// request and response values. It may depend on canonical to preserve each
// turn's semantic input; canonical must not depend on this package.
package responsesnative
