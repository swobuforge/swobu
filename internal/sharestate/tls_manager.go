package sharestate

import (
	"crypto/tls"
	"errors"
	"sync"
)

type TLSManager struct {
	Store     *Store
	mu        sync.RWMutex
	challenge *tls.Certificate
}

func (m *TLSManager) SetChallenge(certificate tls.Certificate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.challenge = &certificate
}
func (m *TLSManager) ClearChallenge() { m.mu.Lock(); defer m.mu.Unlock(); m.challenge = nil }

func (m *TLSManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if m.Store == nil {
		return nil, errors.New("share TLS store is absent")
	}
	endpointID, err := m.Store.EndpointID()
	if err != nil {
		return nil, err
	}
	if hello.ServerName != Hostname(endpointID) {
		return nil, errors.New("unexpected endpoint hostname")
	}
	for _, protocol := range hello.SupportedProtos {
		if protocol == "acme-tls/1" {
			m.mu.RLock()
			challenge := m.challenge
			m.mu.RUnlock()
			if challenge == nil {
				return nil, errors.New("ACME challenge is not active")
			}
			return challenge, nil
		}
	}
	certificate, err := m.Store.TLSCertificate()
	if err != nil {
		return nil, err
	}
	return &certificate, nil
}
