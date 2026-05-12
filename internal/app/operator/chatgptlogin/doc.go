// Package chatgptlogin owns daemon-side operator login-session orchestration
// for ChatGPT subscription login.
//
// The daemon starts OAuth sessions on one control-plane callback surface and
// exchanges authorization codes for access tokens in-process. Only session
// metadata and credential references cross this seam; raw token material is
// persisted through injected credential storage and never returned to callers.
package chatgptlogin
