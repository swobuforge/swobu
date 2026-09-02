package sharetransport

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"time"
)

func SelfSignedClientCertificate(key *ecdsa.PrivateKey, now time.Time) (x509.Certificate, []byte, error) {
	if key == nil {
		return x509.Certificate{}, nil, errors.New("Endpoint identity key is required")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return x509.Certificate{}, nil, err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "swobu-owner"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return x509.Certificate{}, nil, err
	}
	parsed, err := x509.ParseCertificate(der)
	return *parsed, der, err
}
