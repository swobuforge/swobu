package providertest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
)

// CacheSensitiveProjection removes only established execution/locality fields
// from a final provider JSON document. JSON object keys are deterministically
// ordered by encoding/json; array order and every retained value are preserved.
func CacheSensitiveProjection(document carrier.Document) ([]byte, error) {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(document.RawBytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode provider cache projection: %w", err)
	}
	for _, field := range []string{"prompt_cache_key", "session_id", "stream", "stream_options"} {
		delete(root, field)
	}
	projected, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode provider cache projection: %w", err)
	}
	return projected, nil
}
