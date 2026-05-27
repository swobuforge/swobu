package canonical

import "strings"

// CacheRetention is a canonical retention hint used by affinity-style cache
// controls where providers expose retention knobs.
type CacheRetention string

const (
	CacheRetentionUnset    CacheRetention = ""
	CacheRetention5M       CacheRetention = "5m"
	CacheRetention1H       CacheRetention = "1h"
	CacheRetentionInMemory CacheRetention = "in_memory"
	CacheRetention24H      CacheRetention = "24h"
)

// CacheIntent is the canonical cache grammar used by requestpath and adapters.
//
// v1 fields are intentionally minimal and all have at least one active adapter
// consumer:
// - key
// - retention
//
// If a future field has no active provider consumer, it must not be added.
type CacheIntent struct {
	key       string
	retention CacheRetention
}

type CacheIntentParams struct {
	Key       string
	Retention CacheRetention
}

func NewCacheIntent(params CacheIntentParams) CacheIntent {
	key := strings.TrimSpace(params.Key) // swobu:io-string source=domain
	retention := normalizeCacheRetention(params.Retention)
	return CacheIntent{key: key, retention: retention}
}

func normalizeCacheRetention(retention CacheRetention) CacheRetention {
	switch retention {
	case CacheRetention5M, CacheRetention1H, CacheRetentionInMemory, CacheRetention24H:
		return retention
	default:
		return CacheRetentionUnset
	}
}

func (i CacheIntent) Key() string               { return i.key }
func (i CacheIntent) Retention() CacheRetention { return i.retention }

func (i CacheIntent) IsZero() bool {
	return i.key == "" && i.retention == CacheRetentionUnset
}

// CacheIntentFromAffinityKeyAndRetention maps protocol-level affinity fields
// into canonical cache intent.
func CacheIntentFromAffinityKeyAndRetention(key string, retention CacheRetention) CacheIntent {
	normalizedKey := strings.TrimSpace(key) // swobu:io-string source=domain
	normalizedRetention := normalizeCacheRetention(retention)
	return NewCacheIntent(CacheIntentParams{
		Key:       normalizedKey,
		Retention: normalizedRetention,
	})
}
