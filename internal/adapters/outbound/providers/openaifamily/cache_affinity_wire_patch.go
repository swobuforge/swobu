package openaifamily

import (
	"strings"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
)

// CacheAffinityWirePatch writes provider-specific affinity cache fields to wire
type CacheAffinityWirePatch struct {
	Key       string
	Retention string
}

func NewCacheAffinityWirePatch(key string, retention string) core.WirePatch {
	return CacheAffinityWirePatch{
		Key:       strings.TrimSpace(key),       // swobu:io-string source=boundary
		Retention: strings.TrimSpace(retention), // swobu:io-string source=boundary
	}
}

func (p CacheAffinityWirePatch) ApplyEncode(packet *core.WirePacket) error {
	if packet == nil || packet.Payload == nil {
		return nil
	}
	if p.Key != "" {
		packet.Payload["prompt_cache_key"] = p.Key
	}
	if p.Retention != "" {
		packet.Payload["prompt_cache_retention"] = p.Retention
	}
	return nil
}

func (CacheAffinityWirePatch) ApplyDecode(_ *core.WirePacket) error {
	return nil
}
