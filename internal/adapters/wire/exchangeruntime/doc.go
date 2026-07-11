// Package exchangeruntime composes wire protocol family codecs into the
// exchange runtime boundary.
//
// It owns protocol-family selection for one running Swobu process. It does not
// own daemon lifecycle, endpoint resolution, or provider execution.
package exchangeruntime
