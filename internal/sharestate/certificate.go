package sharestate

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

func (s *Store) CSR() ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := endpointID(s.state.Endpoint.PrivateKey)
	if err != nil {
		return nil, "", err
	}
	hostname := Hostname(id)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{hostname}}, s.state.Endpoint.PrivateKey)
	if err != nil {
		return nil, "", fmt.Errorf("create endpoint CSR: %w", err)
	}
	return csr, hostname, nil
}

func (s *Store) InstallCertificateDER(chain [][]byte) error {
	if len(chain) == 0 {
		return errors.New("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return fmt.Errorf("parse endpoint certificate: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	leafSPKI, _ := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	endpointSPKI, _ := x509.MarshalPKIXPublicKey(&s.state.Endpoint.PrivateKey.PublicKey)
	if !bytes.Equal(leafSPKI, endpointSPKI) {
		return errors.New("certificate public key does not match endpoint key")
	}
	id, err := endpointID(s.state.Endpoint.PrivateKey)
	if err != nil {
		return err
	}
	if err := leaf.VerifyHostname(Hostname(id)); err != nil {
		return fmt.Errorf("certificate hostname: %w", err)
	}
	var chainPEM []byte
	for _, der := range chain {
		chainPEM = append(chainPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	previous := s.state.Endpoint.CertificateChain
	s.state.Endpoint.CertificateChain = chainPEM
	if err := s.persist(); err != nil {
		s.state.Endpoint.CertificateChain = previous
		return err
	}
	return nil
}

func (s *Store) TLSCertificate() (tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Endpoint.CertificateChain) == 0 {
		return tls.Certificate{}, errors.New("endpoint certificate is absent")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(s.state.Endpoint.PrivateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(s.state.Endpoint.CertificateChain, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load endpoint certificate: %w", err)
	}
	return certificate, nil
}

func (s *Store) CertificateNotAfter() (time.Time, error) {
	certificate, err := s.TLSCertificate()
	if err != nil {
		return time.Time{}, err
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return time.Time{}, err
	}
	return leaf.NotAfter, nil
}

func RenewalAt(notAfter time.Time, endpointID string) time.Time {
	digest := sha256.Sum256([]byte(endpointID))
	seconds := int64(digest[0])<<8 | int64(digest[1])
	window := 20*24*time.Hour + time.Duration(seconds%int64(10*24*time.Hour/time.Second))*time.Second
	return notAfter.Add(-window)
}

func (s *Store) HasActiveGrants() bool {
	return len(s.ActiveGrants()) > 0
}

func (s *Store) CertificateUsable(now time.Time) bool {
	notAfter, err := s.CertificateNotAfter()
	if err != nil {
		return false
	}
	endpointID, err := s.EndpointID()
	if err != nil {
		return false
	}
	return now.Before(RenewalAt(notAfter, endpointID))
}
