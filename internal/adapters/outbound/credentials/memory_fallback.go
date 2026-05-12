package credentials

import (
	"strings"
	"sync"
)

var memoryFallbackSecrets sync.Map

func memoryFallbackKey(providerSpec string, keyName string) string {
	return strings.TrimSpace(strings.ToLower(providerSpec)) + "::" + strings.TrimSpace(keyName)
}

func setMemoryFallbackSecret(providerSpec string, keyName string, secret string) {
	memoryFallbackSecrets.Store(memoryFallbackKey(providerSpec, keyName), strings.TrimSpace(secret))
}

func getMemoryFallbackSecret(providerSpec string, keyName string) (string, bool) {
	raw, ok := memoryFallbackSecrets.Load(memoryFallbackKey(providerSpec, keyName))
	if !ok {
		return "", false
	}
	val, ok := raw.(string)
	if !ok || strings.TrimSpace(val) == "" {
		return "", false
	}
	return val, true
}
