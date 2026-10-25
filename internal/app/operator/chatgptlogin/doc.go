// Package chatgptlogin owns daemon-side operator login-session orchestration
// for ChatGPT subscription login.
//
// The daemon starts OAuth sessions through canonical operator session transport
// and exchanges authorization codes for access tokens in-process. Only session
// metadata and credential references cross this seam; raw token material is
// persisted through injected credential storage and never returned to callers.
// The provider-registered callback is the service-owned loopback listener, not a
// daemon ServeMux route. Its unpredictable state matches only a live in-process
// session. Restart leaves old state unmatched; expiry and cancellation leave a
// terminal session that rejects the callback. Shutdown cancels pending sessions
// and closes the listener. Callback URLs are never session-control aliases.
//
// Callback port contract:
//   - primary callback listener: 127.0.0.1:1455
//   - fallback callback listener: 127.0.0.1:1457
//   - OAuth redirect base must therefore resolve to localhost:1455 or
//     localhost:1457 only for production browser sessions. Tests may inject an
//     isolated loopback address.
//   - no ephemeral/dynamic callback-port fallback is allowed in this package.
//
// If both contract ports are busy, browser-login start must fail with an
// explicit device-auth hint.
package chatgptlogin
