package bedrock

import "testing"

func TestInspectBedrockRequestShapeLocatesRejectedInputItemWithoutValues(t *testing.T) {
	body := []byte(`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"private user text"}]},{"type":"message","role":"assistant","status":"completed","content":[{"type":"input_text","text":"private assistant text"}]}]}`)
	column := 1
	for index, value := range body {
		if index > 100 && value == 'p' {
			column = index + 1
			break
		}
	}
	diagnostic := inspectBedrockRequestShape(body, "Invalid 'input': value did not match any expected variant at line 1 column "+itoaForTest(column))
	if diagnostic.RequestBytes != len(body) || diagnostic.InputShape != "array" || diagnostic.InputItemCount != 2 {
		t.Fatalf("request diagnostic = %#v", diagnostic)
	}
	if diagnostic.ColumnItemIndex != 1 || diagnostic.ColumnItemType != "message" || diagnostic.ColumnItemRole != "assistant" {
		t.Fatalf("column item diagnostic = %#v", diagnostic)
	}
	if diagnostic.ColumnContent != "input_text" || diagnostic.ColumnItemBytes == 0 {
		t.Fatalf("column content diagnostic = %#v", diagnostic)
	}
	if diagnostic.InputTypeCounts != "message=2" {
		t.Fatalf("input type counts = %q", diagnostic.InputTypeCounts)
	}
}

func itoaForTest(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
