// Package exchangeruntime composes wire protocol family codecs into the
// exchange runtime boundary.
//
// It owns explicit protocol-family bundle composition and shared provider
// request-path selection for one running Swobu process. It does not own daemon
// lifecycle, endpoint resolution, or provider execution, and it does not act
// as a registry-style switchboard.
package exchangeruntime
