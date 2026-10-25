package connect

import accessapp "github.com/swobuforge/swobu/internal/app/operator/access"

// Structural review tests isolate credential authority; authenticated file and
// admission journeys use the real Access service separately.
type testCredentials struct{}

func (testCredentials) ClientPermission(string, string) (string, error)         { return "", nil }
func (testCredentials) ClientCredential(string, string, string) (string, error) { return "", nil }
func (testCredentials) IssueClientCredentialChecked(string, string, string, string, func() error) (accessapp.IssueResult, error) {
	panic("unexpected issuance in structural test")
}
func (testCredentials) ClientCredentialIssuance(string, string, string) (string, error) {
	return "", nil
}
