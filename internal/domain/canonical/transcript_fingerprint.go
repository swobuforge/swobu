package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
)

// TranscriptFingerprint is a candidate index for one visible transcript
// prefix. Verification material is recomputed only for digest candidates so
// storing all prefixes remains linear in transcript length.
type TranscriptFingerprint struct {
	ItemCount int
	Sum       [sha256.Size]byte
	Stable    bool
}

const transcriptFingerprintVersion = "swobu-transcript-prefix-v1"

func TranscriptPrefixFingerprints(request CanonicalRequest) ([]TranscriptFingerprint, error) {
	projector := semanticProjector{calls: make(map[string]uint64), stable: true}
	var material bytes.Buffer
	writeField(&material, "version", []byte(transcriptFingerprintVersion))
	out := make([]TranscriptFingerprint, 0, len(request.Items())+1)
	out = append(out, newTranscriptFingerprint(0, material.Bytes(), true))
	for index, item := range request.Items() {
		encoded, err := projector.item(item)
		if err != nil {
			return nil, fmt.Errorf("transcript fingerprint item %d: %w", index, err)
		}
		writeField(&material, "item", encoded)
		out = append(out, newTranscriptFingerprint(index+1, material.Bytes(), projector.stable))
	}
	return out, nil
}

func newTranscriptFingerprint(count int, material []byte, stable bool) TranscriptFingerprint {
	return TranscriptFingerprint{ItemCount: count, Sum: sha256.Sum256(material), Stable: stable}
}

// TranscriptPrefixVerificationBytes recomputes collision-verification material
// for one candidate prefix instead of retaining every cumulative byte slice.
func TranscriptPrefixVerificationBytes(request CanonicalRequest, itemCount int) ([]byte, error) {
	items := request.Items()
	if itemCount < 0 || itemCount > len(items) {
		return nil, fmt.Errorf("transcript verification item count %d is out of range", itemCount)
	}
	projector := semanticProjector{calls: make(map[string]uint64), stable: true}
	var material bytes.Buffer
	writeField(&material, "version", []byte(transcriptFingerprintVersion))
	for index, item := range items[:itemCount] {
		encoded, err := projector.item(item)
		if err != nil {
			return nil, fmt.Errorf("transcript verification item %d: %w", index, err)
		}
		writeField(&material, "item", encoded)
	}
	return append([]byte(nil), material.Bytes()...), nil
}

// InvocationFingerprint identifies the complete current invocation semantics,
// including ordered request bands and transcript.
type InvocationFingerprint struct {
	Sum   [sha256.Size]byte
	Bytes []byte
}

func FingerprintInvocation(request CanonicalRequest) (InvocationFingerprint, error) {
	if _, ok := request.PreviousResponse(); ok {
		return InvocationFingerprint{}, fmt.Errorf("invocation fingerprint requires a materialized request without previous response")
	}
	var out bytes.Buffer
	writeField(&out, "version", []byte("swobu-invocation-v1"))
	writeSpecifiedString(&out, "model", request.ModelField())
	if instructions, ok := request.InstructionsField().Get(); !ok {
		writeField(&out, "instructions-omitted", nil)
	} else {
		for _, instruction := range instructions.Instructions() {
			writeField(&out, "instruction-role", []byte(instruction.Role()))
			writeField(&out, "instruction-text", []byte(instruction.Text()))
		}
	}
	if tools, ok := request.ToolsField().Get(); !ok {
		writeField(&out, "tools-omitted", nil)
	} else {
		writeField(&out, "tools-specified", nil)
		for _, declaration := range tools.Declarations() {
			encoded, err := projectToolDeclaration(declaration)
			if err != nil {
				return InvocationFingerprint{}, err
			}
			writeField(&out, "tool", encoded)
		}
	}
	if policy, ok := request.ToolPolicyField().Get(); ok {
		writeField(&out, "tool-policy", []byte(policy.Mode))
		if key, hasKey := policy.SpecificID(); hasKey {
			writeField(&out, "tool-policy-key", []byte(key.String()))
		}
	} else {
		writeField(&out, "tool-policy-omitted", nil)
	}
	if batch, ok := request.ToolCallBatchField().Get(); ok {
		writeField(&out, "tool-concurrency", []byte(batch.Mode))
	} else {
		writeField(&out, "tool-concurrency-omitted", nil)
	}
	controls := request.Controls()
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		writeField(&out, "max-output-tokens", []byte(strconv.Itoa(value)))
	} else {
		writeField(&out, "max-output-tokens-omitted", nil)
	}
	if controls.Limits.StopSequences == nil {
		writeField(&out, "stop-sequences-omitted", nil)
	} else {
		for _, stop := range controls.Limits.StopSequences {
			writeField(&out, "stop-sequence", []byte(stop))
		}
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		writeField(&out, "temperature", []byte(strconv.FormatFloat(value, 'g', -1, 64)))
	} else {
		writeField(&out, "temperature-omitted", nil)
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		writeField(&out, "top-p", []byte(strconv.FormatFloat(value, 'g', -1, 64)))
	} else {
		writeField(&out, "top-p-omitted", nil)
	}
	if format, ok := request.OutputFormatField().Get(); ok {
		writeField(&out, "output-kind", []byte(format.Kind))
		writeField(&out, "output-name", []byte(format.Name))
		writeField(&out, "output-description", []byte(format.Description))
		writeField(&out, "output-schema", []byte(format.Schema.RawObject()))
		if format.Strict {
			writeField(&out, "output-strict", []byte{1})
		} else {
			writeField(&out, "output-strict", []byte{0})
		}
	} else {
		writeField(&out, "output-omitted", nil)
	}
	projector := semanticProjector{calls: make(map[string]uint64), stable: true}
	for index, item := range request.Items() {
		encoded, err := projector.item(item)
		if err != nil {
			return InvocationFingerprint{}, fmt.Errorf("invocation fingerprint item %d: %w", index, err)
		}
		writeField(&out, "item", encoded)
	}
	material := append([]byte(nil), out.Bytes()...)
	return InvocationFingerprint{Sum: sha256.Sum256(material), Bytes: material}, nil
}

func writeSpecifiedString(out *bytes.Buffer, tag string, field Specified[string]) {
	value, ok := field.Get()
	if !ok {
		writeField(out, tag+"-omitted", nil)
		return
	}
	writeField(out, tag, []byte(value))
}

type semanticProjector struct {
	calls    map[string]uint64
	nextCall uint64
	stable   bool
}

func (p *semanticProjector) item(item CanonicalItem) ([]byte, error) {
	var out bytes.Buffer
	switch item.Kind() {
	case ItemKindMessage:
		message, _ := item.Message()
		writeField(&out, "kind", []byte(ItemKindMessage))
		writeField(&out, "author", []byte(message.Role()))
		if err := p.content(&out, message.Content()); err != nil {
			return nil, err
		}
	case ItemKindToolCall:
		call, _ := item.ToolCall()
		key := call.CallID().String()
		if _, duplicate := p.calls[key]; duplicate {
			return nil, fmt.Errorf("duplicate tool call id %q", key)
		}
		ordinal := p.nextCall
		p.nextCall++
		p.calls[key] = ordinal
		writeField(&out, "kind", []byte(ItemKindToolCall))
		writeOrdinal(&out, ordinal)
		writeField(&out, "tool", []byte(call.Tool().String()))
		input := call.Input()
		if object, ok := input.Object(); ok {
			writeField(&out, "input-object", object.Bytes())
		} else if text, ok := input.Text(); ok {
			writeField(&out, "input-text", []byte(text))
		} else {
			return nil, fmt.Errorf("tool call has invalid input")
		}
	case ItemKindToolResult:
		result, _ := item.ToolResult()
		key := result.CallID().String()
		ordinal, ok := p.calls[key]
		if !ok {
			return nil, fmt.Errorf("tool result references unmatched call id %q", key)
		}
		delete(p.calls, key)
		writeField(&out, "kind", []byte(ItemKindToolResult))
		writeOrdinal(&out, ordinal)
		if result.IsError() {
			writeField(&out, "error", []byte{1})
		} else {
			writeField(&out, "error", []byte{0})
		}
		if err := p.toolResultContent(&out, result.Content()); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported canonical item kind %q", item.Kind())
	}
	return out.Bytes(), nil
}

func (p *semanticProjector) content(out *bytes.Buffer, parts []MessagePart) error {
	for _, part := range parts {
		if err := p.projectPart(out, part.Kind(), part.Text, part.Image); err != nil {
			return err
		}
	}
	return nil
}

func (p *semanticProjector) toolResultContent(out *bytes.Buffer, parts []ToolResultPart) error {
	for _, part := range parts {
		if err := p.projectPart(out, part.Kind(), part.Text, part.Image); err != nil {
			return err
		}
	}
	return nil
}

func (p *semanticProjector) projectPart(out *bytes.Buffer, kind PartKind, textFn func() (TextPart, bool), imageFn func() (ImagePart, bool)) error {
	switch kind {
	case PartKindText:
		text, _ := textFn()
		writeField(out, "text", []byte(text.Text()))
	case PartKindImage:
		image, _ := imageFn()
		if detail, ok := image.Detail().Get(); ok {
			writeField(out, "image-detail", []byte(detail))
		} else {
			writeField(out, "image-detail-omitted", nil)
		}
		source := image.Source()
		if rawURL, ok := source.URL(); ok {
			p.stable = false
			writeField(out, "image-source", []byte("url"))
			writeField(out, "image-url", []byte(rawURL.String()))
		} else if media, ok := source.Inline(); ok {
			data := media.Data()
			digest := sha256.Sum256(data)
			writeField(out, "image-source", []byte("inline"))
			writeField(out, "image-media-type", []byte(media.MediaType()))
			writeField(out, "image-byte-length", []byte(strconv.Itoa(len(data))))
			writeField(out, "image-digest", digest[:])
		} else {
			return fmt.Errorf("image content has invalid source")
		}
	default:
		return fmt.Errorf("unsupported content kind %q", kind)
	}
	return nil
}

func projectToolDeclaration(declaration ToolDeclaration) ([]byte, error) {
	if declaration.Kind() == "" {
		return nil, fmt.Errorf("invalid tool declaration")
	}
	var out bytes.Buffer
	writeField(&out, "key", []byte(declaration.Key().String()))
	if function, ok := declaration.Function(); ok {
		writeField(&out, "description", []byte(function.Description()))
		if strict, set := function.Strict().Get(); !set {
			writeField(&out, "strict-omitted", nil)
		} else if strict {
			writeField(&out, "strict", []byte{1})
		} else {
			writeField(&out, "strict", []byte{0})
		}
		if err := writeObjectField(&out, "schema", function.InputSchema().RawObject()); err != nil {
			return nil, err
		}
	} else if custom, ok := declaration.Custom(); ok {
		writeField(&out, "description", []byte(custom.Description()))
		if err := writeObjectField(&out, "format", custom.Format().RawObject()); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unsupported tool declaration")
	}
	return out.Bytes(), nil
}

func writeObjectField(out *bytes.Buffer, tag, raw string) error {
	if raw == "" {
		writeField(out, tag, nil)
		return nil
	}
	object, err := ParseJSONObject([]byte(raw))
	if err != nil {
		return fmt.Errorf("tool declaration %s is invalid: %w", tag, err)
	}
	writeField(out, tag, object.Bytes())
	return nil
}

func writeField(out *bytes.Buffer, tag string, value []byte) {
	writeBytes(out, []byte(tag))
	writeBytes(out, value)
}
func writeBytes(out *bytes.Buffer, value []byte) {
	var length [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(length[:], uint64(len(value)))
	out.Write(length[:n])
	out.Write(value)
}
func writeOrdinal(out *bytes.Buffer, ordinal uint64) {
	var value [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(value[:], ordinal)
	writeField(out, "call", value[:n])
}
