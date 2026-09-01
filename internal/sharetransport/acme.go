package sharetransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/shareprotocol"
)

func ProvisionCertificate(ctx context.Context, control io.ReadWriter, store *sharestate.Store, manager *sharestate.TLSManager) error {
	if store == nil || manager == nil {
		return errors.New("Owner certificate dependencies are required")
	}
	csr, _, err := store.CSR()
	if err != nil {
		return err
	}
	codec := shareprotocol.NewCodec(control)
	request := shareprotocol.Message{Type: "certificate_request", CSR: base64.RawStdEncoding.EncodeToString(csr)}
	if certificate, certificateErr := store.TLSCertificate(); certificateErr == nil {
		request.PriorChain = make([]string, 0, len(certificate.Certificate))
		for _, der := range certificate.Certificate {
			request.PriorChain = append(request.PriorChain, base64.RawStdEncoding.EncodeToString(der))
		}
	}
	if err := codec.Write(request); err != nil {
		return err
	}
	challenge, err := codec.Read()
	if err != nil {
		return err
	}
	if challenge.Type != "challenge" {
		return fmt.Errorf("Relay certificate provisioning failed: %s", challenge.Error)
	}
	temporary, err := decodeTemporaryCertificate(challenge.CertificateChain, challenge.ChallengePrivateKey)
	if err != nil {
		return err
	}
	manager.SetChallenge(temporary)
	defer manager.ClearChallenge()
	if err := codec.Write(shareprotocol.Message{Type: "challenge_ready"}); err != nil {
		return err
	}
	certificate, err := codec.Read()
	if err != nil {
		return err
	}
	if certificate.Type != "certificate" {
		return fmt.Errorf("Relay certificate provisioning failed: %s", certificate.Error)
	}
	chain := make([][]byte, 0, len(certificate.CertificateChain))
	for _, encoded := range certificate.CertificateChain {
		der, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return err
		}
		chain = append(chain, der)
	}
	if err := store.InstallCertificateDER(chain); err != nil {
		return err
	}
	return codec.Write(shareprotocol.Message{Type: "certificate_installed"})
}

func SignalSessionReady(control io.ReadWriter) error {
	return shareprotocol.NewCodec(control).Write(shareprotocol.Message{Type: "session_ready"})
}

func decodeTemporaryCertificate(chain []string, encodedKey string) (tls.Certificate, error) {
	certificate := tls.Certificate{}
	for _, encoded := range chain {
		der, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return tls.Certificate{}, err
		}
		certificate.Certificate = append(certificate.Certificate, der)
	}
	keyDER, err := base64.RawStdEncoding.DecodeString(encodedKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate.PrivateKey = key
	return certificate, nil
}
