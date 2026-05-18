package chatgpt

import "testing"

func TestChatGPTTierModelIDs_HasPlusTier(t *testing.T) {
	models, ok := chatGPTTierModelIDs("plus")
	if !ok {
		t.Fatal("plus tier missing")
	}
	if len(models) == 0 {
		t.Fatal("plus tier models empty")
	}
}
