package sharetransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/shareprotocol"
)

func TestProvisionCertificateProtocolV2ReceivesCertificateDirectlyAndKeepsOwnerKey(t *testing.T) {
	oldSystemCertPool := systemCertPool
	defer func() { systemCertPool = oldSystemCertPool }()
	trustedRoots := x509.NewCertPool()
	systemCertPool = func() (*x509.CertPool, error) { return trustedRoots, nil }
	store, err := sharestate.Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	server, owner := net.Pipe()
	done := make(chan error, 1)
	go func() {
		codec := shareprotocol.NewCodec(server)
		request, err := codec.Read()
		if err != nil {
			done <- err
			return
		}
		if request.Type != "certificate_request" {
			done <- errUnexpectedMessage(request.Type)
			return
		}
		csrDER, err := base64.RawStdEncoding.DecodeString(request.CSR)
		if err != nil {
			done <- err
			return
		}
		csr, err := x509.ParseCertificateRequest(csrDER)
		if err != nil {
			done <- err
			return
		}
		key := csr.PublicKey.(*ecdsa.PublicKey)
		now := time.Now()
		template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: csr.DNSNames, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}
		caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		caTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}
		caDER, _ := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
		ca, _ := x509.ParseCertificate(caDER)
		trustedRoots.AddCert(ca)
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, key, caKey)
		if err != nil {
			done <- err
			return
		}
		if err = codec.Write(shareprotocol.Message{Type: "certificate", CertificateChain: []string{base64.RawStdEncoding.EncodeToString(der), base64.RawStdEncoding.EncodeToString(caDER)}}); err != nil {
			done <- err
			return
		}
		installed, err := codec.Read()
		if err != nil {
			done <- err
			return
		}
		if installed.Type != "certificate_installed" {
			done <- errUnexpectedMessage(installed.Type)
			return
		}
		done <- nil
	}()
	if err := ProvisionCertificate(context.Background(), owner, store, trustedRoots); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	certificate, err := store.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	private := certificate.PrivateKey.(*ecdsa.PrivateKey)
	if !private.PublicKey.Equal(leaf.PublicKey) {
		t.Fatal("installed certificate does not match Owner-held private key")
	}
}

type unexpectedMessage string

func (e unexpectedMessage) Error() string     { return "unexpected message: " + string(e) }
func errUnexpectedMessage(value string) error { return unexpectedMessage(value) }
