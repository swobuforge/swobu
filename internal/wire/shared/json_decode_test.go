package shared

import "testing"

func TestDecodeExtensibleRequestObjectPreservesObjectPerimeter(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "unknown accepted", raw: `{"known":true,"future":{"nested":1}}`},
		{name: "known type error", raw: `{"known":"yes"}`, wantErr: true},
		{name: "malformed", raw: `{"known":`, wantErr: true},
		{name: "array", raw: `[]`, wantErr: true},
		{name: "scalar", raw: `true`, wantErr: true},
		{name: "null", raw: `null`, wantErr: true},
		{name: "empty", raw: ``, wantErr: true},
		{name: "trailing", raw: `{"known":true} {}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var target struct {
				Known bool `json:"known"`
			}
			err := DecodeExtensibleRequestObject([]byte(test.raw), &target, "test request")
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, test.wantErr)
			}
			if !test.wantErr && !target.Known {
				t.Fatal("known member was not decoded")
			}
		})
	}
}
