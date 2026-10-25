package relaytransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/swobuforge/swobu/internal/relaystate"
	"github.com/swobuforge/swobu/relayprotocol"
)

type CertificateProvisionError struct {
	Message    string
	RetryAfter time.Duration
}

var systemCertPool = x509.SystemCertPool

func (e *CertificateProvisionError) Error() string {
	return "Relay certificate provisioning failed: " + e.Message
}

func provisioningResponseError(message relayprotocol.Message) error {
	if message.Type == "error" {
		retryAfter := time.Duration(0)
		if message.RetryAfterSeconds > 0 {
			retryAfter = time.Duration(message.RetryAfterSeconds) * time.Second
		}
		return &CertificateProvisionError{Message: message.Error, RetryAfter: retryAfter}
	}
	return fmt.Errorf("Relay certificate provisioning failed: %s", message.Error)
}

func ProvisionCertificate(ctx context.Context, control io.ReadWriter, store *relaystate.Store, roots *x509.CertPool) error {
	if store == nil {
		return errors.New("Owner certificate store is required")
	}
	identity, err := store.EndpointID()
	if err != nil {
		return err
	}
	candidate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Endpoint TLS key: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{relaystate.Hostname(identity)}}, candidate)
	if err != nil {
		return fmt.Errorf("create Endpoint TLS CSR: %w", err)
	}
	codec := relayprotocol.NewCodec(control)
	request := relayprotocol.Message{Type: "certificate_request", CSR: base64.RawStdEncoding.EncodeToString(csr)}
	if certificate, certificateErr := store.TLSCertificate(); certificateErr == nil {
		request.PriorChain = make([]string, 0, len(certificate.Certificate))
		for _, der := range certificate.Certificate {
			request.PriorChain = append(request.PriorChain, base64.RawStdEncoding.EncodeToString(der))
		}
	}
	if err := codec.Write(request); err != nil {
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
	if roots == nil {
		roots, err = systemCertPool()
		if err != nil {
			return fmt.Errorf("load system certificate roots: %w", err)
		}
	}
	if err := store.InstallTLSCredential(candidate, chain, roots); err != nil {
		return err
	}
	return codec.Write(relayprotocol.Message{Type: "certificate_installed"})
}

func SignalSessionReady(control io.ReadWriter) error {
	return relayprotocol.NewCodec(control).Write(relayprotocol.Message{Type: "session_ready"})
}
