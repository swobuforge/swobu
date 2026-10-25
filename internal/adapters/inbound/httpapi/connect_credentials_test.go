package httpapi

import accessapp "github.com/swobuforge/swobu/internal/app/operator/access"

type connectTestCredentials struct{}

func (connectTestCredentials) ClientPermission(string, string) (string, error) { return "", nil }
func (connectTestCredentials) ClientCredential(string, string, string) (string, error) {
	return "", nil
}
func (connectTestCredentials) IssueClientCredentialChecked(string, string, string, string, func() error) (accessapp.IssueResult, error) {
	panic("unexpected issuance in structural test")
}
func (connectTestCredentials) ClientCredentialIssuance(string, string, string) (string, error) {
	return "", nil
}
