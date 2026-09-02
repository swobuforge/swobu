package sharestate

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

func (s *Store) InstallTLSCredential(privateKey *ecdsa.PrivateKey, chain [][]byte) error {
	if privateKey == nil || privateKey.Curve != elliptic.P256() {
		return errors.New("Endpoint TLS key must be ECDSA P-256")
	}
	if len(chain) == 0 {
		return errors.New("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return fmt.Errorf("parse endpoint certificate: %w", err)
	}
	leafSPKI, _ := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	keySPKI, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if !bytes.Equal(leafSPKI, keySPKI) {
		return errors.New("certificate public key does not match Endpoint TLS key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := endpointID(s.state.Endpoint.IdentityPrivateKey)
	if err != nil {
		return err
	}
	if err := leaf.VerifyHostname(Hostname(id)); err != nil {
		return fmt.Errorf("certificate hostname: %w", err)
	}
	var chainPEM []byte
	for _, der := range chain {
		if _, err := x509.ParseCertificate(der); err != nil {
			return fmt.Errorf("parse endpoint certificate chain: %w", err)
		}
		chainPEM = append(chainPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	previous := s.state.Endpoint
	s.state.Endpoint.TLSCredential = &TLSCredential{PrivateKey: privateKey, CertificateChain: chainPEM}
	s.state.Endpoint.CertificateFailureStreak = 0
	s.state.Endpoint.NextCertificateAttempt = time.Time{}
	if err := s.persist(); err != nil {
		s.state.Endpoint = previous
		return err
	}
	return nil
}

func (s *Store) TLSCertificate() (tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential := s.state.Endpoint.TLSCredential
	if credential == nil {
		return tls.Certificate{}, errors.New("Endpoint TLS credential is absent")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(credential.PrivateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(credential.CertificateChain, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load Endpoint TLS credential: %w", err)
	}
	return certificate, nil
}

func ReplacementAt(notBefore, notAfter time.Time, endpointID string) time.Time {
	lifetime := notAfter.Sub(notBefore)
	base := notBefore.Add(lifetime * 2 / 3)
	digest := sha256.Sum256([]byte(endpointID))
	fraction := uint64(digest[0])<<8 | uint64(digest[1])
	// Spread replacement over the next five percent of certificate lifetime.
	jitterWindow := lifetime / 20
	jitter := time.Duration(uint64(jitterWindow) * fraction / (1 << 16))
	return base.Add(jitter)
}

func certificateRetryDelay(endpointID string, streak uint32) time.Duration {
	base := 24 * time.Hour
	switch streak {
	case 1:
		base = time.Minute
	case 2:
		base = 10 * time.Minute
	case 3:
		base = 100 * time.Minute
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", endpointID, streak)))
	value := uint64(digest[0])<<56 | uint64(digest[1])<<48 | uint64(digest[2])<<40 | uint64(digest[3])<<32 |
		uint64(digest[4])<<24 | uint64(digest[5])<<16 | uint64(digest[6])<<8 | uint64(digest[7])
	return base + time.Duration(value%uint64(base/5+1))
}

func (s *Store) HasActiveGrants() bool { return len(s.ActiveGrants()) > 0 }

type CertificateState struct {
	Valid   bool
	Due     bool
	RetryAt time.Time
}

func (c CertificateState) CanAttempt(now time.Time) bool {
	return c.RetryAt.IsZero() || !now.Before(c.RetryAt)
}

func (s *Store) CertificateState(now time.Time) CertificateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := CertificateState{Due: true, RetryAt: s.state.Endpoint.NextCertificateAttempt}
	if s.state.Endpoint.TLSCredential == nil {
		return state
	}
	block, _ := pem.Decode(s.state.Endpoint.TLSCredential.CertificateChain)
	if block == nil {
		return state
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return state
	}
	state.Valid = !now.Before(leaf.NotBefore) && now.Before(leaf.NotAfter)
	if !state.Valid {
		return state
	}
	id, err := endpointID(s.state.Endpoint.IdentityPrivateKey)
	if err == nil {
		state.Due = !now.Before(ReplacementAt(leaf.NotBefore, leaf.NotAfter, id))
	}
	return state
}

func (s *Store) RecordCertificateFailure(now time.Time, retryAfter time.Duration) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Endpoint
	if s.state.Endpoint.CertificateFailureStreak < ^uint32(0) {
		s.state.Endpoint.CertificateFailureStreak++
	}
	id, err := endpointID(s.state.Endpoint.IdentityPrivateKey)
	if err != nil {
		return time.Time{}, err
	}
	delay := certificateRetryDelay(id, s.state.Endpoint.CertificateFailureStreak)
	if retryAfter > delay {
		delay = retryAfter
	}
	deadline := now.UTC().Add(delay)
	s.state.Endpoint.NextCertificateAttempt = deadline
	if err := s.persist(); err != nil {
		s.state.Endpoint = previous
		return time.Time{}, err
	}
	return deadline, nil
}
