package sharestate

import (
	"crypto/tls"
	"errors"
)

type TLSManager struct {
	Store *Store
}

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
	certificate, err := m.Store.TLSCertificate()
	if err != nil {
		return nil, err
	}
	return &certificate, nil
}
