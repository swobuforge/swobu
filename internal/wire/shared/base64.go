package shared

import (
	"encoding/base64"
	"errors"
)

// ErrBase64DecodedLimit reports that decoding would exceed an ingress-owned
// allocation bound. The check happens against encoded length before DecodeString
// allocates its destination buffer.
var ErrBase64DecodedLimit = errors.New("base64 decoded value exceeds limit")

// DecodeBase64Limited decodes standard padded base64 after proving its decoded
// allocation cannot exceed maxDecodedBytes.
func DecodeBase64Limited(encoded string, maxDecodedBytes int) ([]byte, error) {
	if maxDecodedBytes < 0 || len(encoded) > base64.StdEncoding.EncodedLen(maxDecodedBytes) {
		return nil, ErrBase64DecodedLimit
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if maxDecodedBytes >= 0 && len(decoded) > maxDecodedBytes {
		return nil, ErrBase64DecodedLimit
	}
	return decoded, nil
}
