package connect

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func credentialIssuanceKey(key string) string {
	return fmt.Sprintf("connect-%x", sha256.Sum256([]byte("client-credential\x00"+key)))
}

// CredentialDeliveryError exposes only public recovery metadata. The original
// one-shot bearer cannot be recovered or silently replaced after uncertainty.
type CredentialDeliveryError struct {
	Principal  string
	Credential string
}

func (err *CredentialDeliveryError) Error() string {
	return fmt.Sprintf("Connect credential delivery is uncertain. Inspect Principal %s credential %s. If it was not delivered, run swobu credential revoke --principal %s --credential %s before explicitly reviewing a new Connect attempt", err.Principal, err.Credential, err.Principal, err.Credential)
}

func credentialDeliveryError(principal, credential string) error {
	return &CredentialDeliveryError{Principal: principal, Credential: credential}
}

func (s *Service) credentialRecovery(err error, key string, target clientconnect.Target) error {
	credential, readErr := s.credentials.ClientCredentialIssuance(target.Principal(), target.WorkspaceSlug(), credentialIssuanceKey(key))
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if credential != "" {
		return errors.Join(err, credentialDeliveryError(target.Principal(), credential))
	}
	return err
}
