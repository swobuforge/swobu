package sharestate

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestCertificateValidityRenewalAndRetryAreIndependent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	id, _ := store.EndpointID()
	tlsKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(-65 * 24 * time.Hour),
		NotAfter: now.Add(25 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
	}, &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(-65 * 24 * time.Hour), NotAfter: now.Add(25 * 24 * time.Hour)}, &tlsKey.PublicKey, tlsKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallTLSCredential(tlsKey, [][]byte{der}); err != nil {
		t.Fatal(err)
	}
	if !store.CertificateState(now).Valid {
		t.Fatal("certificate with 25 days remaining is not valid")
	}
	if !store.CertificateState(now).Due {
		t.Fatal("certificate with 25 days remaining is not renewal-due")
	}
	if !store.CertificateState(now).CanAttempt(now) {
		t.Fatal("fresh certificate state blocks attempt")
	}
}

func TestNotYetValidCertificateRemainsDue(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "share.json"))
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id, _ := store.EndpointID()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(99), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(99), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallTLSCredential(key, [][]byte{der}); err != nil {
		t.Fatal(err)
	}
	state := store.CertificateState(now)
	if state.Valid || !state.Due {
		t.Fatalf("future credential state = %+v, want invalid and due", state)
	}
}

func TestCertificateFailureBackoffPersistsAndInstallClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	store, _ := Open(path)
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	id, _ := store.EndpointID()
	bases := []time.Duration{time.Minute, 10 * time.Minute, 100 * time.Minute, 24 * time.Hour, 24 * time.Hour}
	for i, base := range bases {
		d := certificateRetryDelay(id, uint32(i+1))
		if d < base || d > base+base/5 {
			t.Fatalf("failure %d delay %s outside [%s,%s]", i+1, d, base, base+base/5)
		}
	}
	deadline, err := store.RecordCertificateFailure(now, 6*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deadline.Before(now.Add(6 * time.Hour)) {
		t.Fatalf("Retry-After did not win: %s", deadline)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.CertificateState(now).CanAttempt(now) {
		t.Fatal("restart lost attempt deadline")
	}
	if !reopened.CertificateState(deadline).CanAttempt(deadline) {
		t.Fatal("attempt blocked at deadline equality")
	}

	id, _ = reopened.EndpointID()
	tlsKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{Hostname(id)}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &tlsKey.PublicKey, tlsKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.InstallTLSCredential(tlsKey, [][]byte{der}); err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.Snapshot().Endpoint
	if snapshot.CertificateFailureStreak != 0 || !snapshot.NextCertificateAttempt.IsZero() {
		t.Fatalf("install retained retry state: %+v", snapshot)
	}
}

func TestCertificateFailureStreakPersistsConsecutiveHistory(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "share.json"))
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		if _, err := store.RecordCertificateFailure(now.Add(time.Duration(i)*24*time.Hour), 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.Snapshot().Endpoint.CertificateFailureStreak; got != 20 {
		t.Fatalf("failure streak = %d, want 20", got)
	}
}

func TestReplacementAtScalesWithCertificateLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{20 * 24 * time.Hour, 45 * 24 * time.Hour, 90 * 24 * time.Hour} {
		notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		got := ReplacementAt(notBefore, notBefore.Add(lifetime), "endpoint")
		offset := got.Sub(notBefore)
		if offset < lifetime*2/3 || offset >= lifetime*2/3+lifetime/20 {
			t.Fatalf("lifetime %s replacement offset = %s", lifetime, offset)
		}
	}
}

func TestSchemaV1IsRejectedWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	raw := []byte("{\"schema_version\":1,\"endpoint_private_key_pem\":\"legacy\"}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || err.Error() != "unsupported share state schema version 1" {
		t.Fatalf("Open V1 = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) {
		t.Fatal("V1 rejection mutated state")
	}
}

func TestTLSCredentialReplacementKeepsIdentityAndRotatesLeafKey(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "share.json"))
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	identityBefore, _ := store.EndpointID()
	workspace, _ := routing.ParseWorkspaceSlug("personal")
	route, _ := routing.ParseRouteName("coding")
	grant, err := store.Issue(workspace, route, ExpirySevenDays)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var prior *ecdsa.PrivateKey
	for serial := int64(1); serial <= 2; serial++ {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{Hostname(identityBefore)}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{Hostname(identityBefore)}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.InstallTLSCredential(key, [][]byte{der}); err != nil {
			t.Fatal(err)
		}
		if store.Snapshot().Endpoint.IdentityPrivateKey.PublicKey.X.Cmp(key.PublicKey.X) == 0 {
			t.Fatal("TLS key reused identity key")
		}
		if prior != nil && prior.PublicKey.X.Cmp(key.PublicKey.X) == 0 {
			t.Fatal("TLS key did not rotate")
		}
		prior = key
	}
	identityAfter, _ := store.EndpointID()
	if identityAfter != identityBefore {
		t.Fatalf("EndpointID changed: %q -> %q", identityBefore, identityAfter)
	}
	grants := store.Snapshot().Grants
	if len(grants) != 1 || grants[0].Bearer != grant.Bearer {
		t.Fatal("TLS replacement changed Grant")
	}
}
