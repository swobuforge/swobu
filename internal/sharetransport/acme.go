package sharetransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/shareprotocol"
)

type CertificateProvisionError struct {
	Message    string
	RetryAfter time.Duration
}

func (e *CertificateProvisionError) Error() string {
	return "Relay certificate provisioning failed: " + e.Message
}

func provisioningResponseError(message shareprotocol.Message) error {
	if message.Type == "error" {
		retryAfter := time.Duration(0)
		if message.RetryAfterSeconds > 0 {
			retryAfter = time.Duration(message.RetryAfterSeconds) * time.Second
		}
		return &CertificateProvisionError{Message: message.Error, RetryAfter: retryAfter}
	}
	return fmt.Errorf("Relay certificate provisioning failed: %s", message.Error)
}

func ProvisionCertificate(ctx context.Context, control io.ReadWriter, store *sharestate.Store, manager *sharestate.TLSManager) error {
	if store == nil || manager == nil {
		return errors.New("Owner certificate dependencies are required")
	}
	identity, err := store.EndpointID()
	if err != nil {
		return err
	}
	candidate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Endpoint TLS key: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{sharestate.Hostname(identity)}}, candidate)
	if err != nil {
		return fmt.Errorf("create Endpoint TLS CSR: %w", err)
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
		return provisioningResponseError(challenge)
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
		return provisioningResponseError(certificate)
	}
	chain := make([][]byte, 0, len(certificate.CertificateChain))
	for _, encoded := range certificate.CertificateChain {
		der, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return err
		}
		chain = append(chain, der)
	}
	if err := store.InstallTLSCredential(candidate, chain); err != nil {
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
