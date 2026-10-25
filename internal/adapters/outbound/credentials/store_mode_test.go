package credentials

import "testing"

func TestParseCredentialWritePolicy(t *testing.T) {
	tests := []struct {
		raw     string
		wantW   CredentialWritePolicy
		wantErr bool
	}{
		{raw: "", wantW: CredentialWritePolicyFile},
		{raw: "default", wantW: CredentialWritePolicyFile},
		{raw: "keyring", wantW: CredentialWritePolicyKeyring},
		{raw: "KEYRING", wantW: CredentialWritePolicyKeyring},
		{raw: "file", wantW: CredentialWritePolicyFile},
		{raw: "auto", wantErr: true},
		{raw: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseCredentialWritePolicy(tt.raw)
		if (err != nil) != tt.wantErr || !tt.wantErr && got != tt.wantW {
			t.Fatalf("ParseCredentialWritePolicy(%q)=(%q,%v), want %q error=%v", tt.raw, got, err, tt.wantW, tt.wantErr)
		}
	}
}
