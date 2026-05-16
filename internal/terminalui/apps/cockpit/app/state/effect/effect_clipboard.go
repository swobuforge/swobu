package effect

import (
	"context"
	"strings"
	"unicode"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// CopyEndpointValueEffect copies an endpoint value to the clipboard.
type CopyEndpointValueEffect struct{ Value string }

func (cmd CopyEndpointValueEffect) Execute(ctx context.Context) []update.Action {
	msg := copyValueNote(cmd.Value)
	return []update.Action{EndpointCopyNoted{Message: msg}}
}

// CopyAuthSessionURLEffect copies auth session URL and reports auth-local note.
type CopyAuthSessionURLEffect struct {
	OwnerKey string
	Value    string
}

func (cmd CopyAuthSessionURLEffect) Execute(ctx context.Context) []update.Action {
	msg := copyValueNote(normalizeAuthSessionCopyValue(cmd.Value))
	return []update.Action{AuthSessionCopyNoted{OwnerKey: cmd.OwnerKey, Message: msg}}
}

func normalizeAuthSessionCopyValue(value string) string {
	trimmed := strings.TrimSpace(value) // trimlowerlint:allow boundary canonicalization
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, trimmed)
}

// CopyClientBaseURLEffect copies a client base URL to the clipboard.
type CopyClientBaseURLEffect struct{ Value string }

func (cmd CopyClientBaseURLEffect) Execute(ctx context.Context) []update.Action {
	msg := copyValueNote(cmd.Value)
	return []update.Action{ClientCopyNoted{Message: msg}}
}

// LaunchClientEffect attempts to launch the selected local client preset.
type LaunchClientEffect struct {
	BaseURL string
	Preset  string
	ModelID string
}

func (cmd LaunchClientEffect) Execute(ctx context.Context) []update.Action {
	msg := runClientOnceMessage(ctx, cmd.BaseURL, cmd.Preset, cmd.ModelID)
	return []update.Action{ClientLaunchNoted{Message: msg}}
}

// EndpointCopyNoted reports the result of an endpoint copy operation.
type EndpointCopyNoted struct{ Message string }

// AuthSessionCopyNoted reports the result of copying auth session URL.
type AuthSessionCopyNoted struct {
	OwnerKey string
	Message  string
}

// ClientCopyNoted reports the result of a client URL copy operation.
type ClientCopyNoted struct{ Message string }

// ClientLaunchNoted reports the result of a client launch attempt.
type ClientLaunchNoted struct{ Message string }

// CopyHelpDiagnosticsEffect copies help diagnostics text for operator support.
type CopyHelpDiagnosticsEffect struct{ Text string }

func (cmd CopyHelpDiagnosticsEffect) Execute(ctx context.Context) []update.Action {
	msg := copyValueNote(cmd.Text)
	return []update.Action{HelpDiagnosticsCopyNoted{Message: msg}}
}

// HelpDiagnosticsCopyNoted reports help diagnostics copy result.
type HelpDiagnosticsCopyNoted struct{ Message string }
