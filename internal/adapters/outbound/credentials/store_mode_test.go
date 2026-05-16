package credentials

import "testing"

func TestNormalizeCredentialWritePolicy(t *testing.T) {
	tests := []struct {
		raw   string
		wantW CredentialWritePolicy
	}{
		{raw: "", wantW: CredentialWritePolicyAuto},
		{raw: "default", wantW: CredentialWritePolicyAuto},
		{raw: "keyring", wantW: CredentialWritePolicyKeyring},
		{raw: "KEYRING", wantW: CredentialWritePolicyKeyring},
		{raw: "file", wantW: CredentialWritePolicyFile},
		{raw: "auto", wantW: CredentialWritePolicyAuto},
		{raw: "unknown", wantW: CredentialWritePolicyAuto},
	}
	for _, tt := range tests {
		if got := NormalizeCredentialWritePolicy(tt.raw); got != tt.wantW {
			t.Fatalf("NormalizeCredentialWritePolicy(%q)=%q want %q", tt.raw, got, tt.wantW)
		}
	}
}
