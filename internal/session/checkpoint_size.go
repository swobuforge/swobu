package session

import (
	"fmt"
	"strconv"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func validateCheckpointSize(record Checkpoint, limit int64) error {
	// This estimate bounds retained semantic state and owned media; it is not a
	// serialized storage-byte measurement.
	size, err := requestSemanticSize(record.Request)
	if err != nil {
		return err
	}
	if err := record.ResolvedMedia.ValidateForRequest(record.Request); err != nil {
		return err
	}
	for _, asset := range record.ResolvedMedia.assets {
		size += 32 + len(asset.mediaType) + len(asset.bytes)
	}
	for _, binding := range record.ResolvedMedia.bindings {
		size += 32 + len(binding.sourceURL) + 8
	}
	size += 64 + len(record.Response.Response().SwobuID.String()) + len(record.Response.Model()) + len(record.Response.CompletionReason())
	if native := record.Response.Response().Responses; native != nil {
		size += len(native.ProviderResponseID.String()) + len(native.TargetID) + 8
	}
	for _, tokenValue := range []func() (int, bool){record.Response.Usage().InputTokens, record.Response.Usage().OutputTokens, record.Response.Usage().ReasoningTokens, record.Response.Usage().CacheReadTokens, record.Response.Usage().CacheWriteTokens} {
		if value, ok := tokenValue(); ok {
			size += len(strconv.Itoa(value)) + 8
		}
	}
	size, err = addItemBytes(size, record.Response.Items())
	if err != nil {
		return err
	}
	return enforceCheckpointSize(size, limit)
}

// ValidateRequestSize rejects canonical request state that cannot fit within
// the configured checkpoint retention bound.
func ValidateRequestSize(request canonical.CanonicalRequest, limit int64) error {
	size, err := requestSemanticSize(request)
	if err != nil {
		return err
	}
	return enforceCheckpointSize(size, limit)
}

// ValidateResolvedRequestSize includes durable external-media resolution
// bytes already known before provider execution.
func ValidateResolvedRequestSize(request canonical.CanonicalRequest, media ResolvedMedia, limit int64) error {
	size, err := requestSemanticSize(request)
	if err != nil {
		return err
	}
	if media.AssetCount() > 0 || media.BindingCount() > 0 {
		if err := media.ValidateForRequest(request); err != nil {
			return err
		}
	}
	for _, asset := range media.assets {
		size += 32 + len(asset.mediaType) + len(asset.bytes)
	}
	for _, binding := range media.bindings {
		size += 32 + len(binding.sourceURL) + 8
	}
	return enforceCheckpointSize(size, limit)
}

func requestSemanticSize(request canonical.CanonicalRequest) (int, error) {
	size := 64 + len(request.Model())
	for _, instruction := range request.Instructions().Instructions() {
		size += 48 + len(instruction.Role()) + len(instruction.Text())
	}
	for _, tool := range request.Tools() {
		size += 64 + len(tool.Key().String())
		if function, ok := tool.Function(); ok {
			size += len(function.Description()) + len(function.InputSchema().RawObject())
			if strict, specified := function.Strict().Get(); specified {
				size += 2
				_ = strict
			}
		} else if custom, ok := tool.Custom(); ok {
			size += len(custom.Description()) + len(custom.Format().RawObject())
		} else {
			return 0, fmt.Errorf("session checkpoint contains unsupported tool declaration")
		}
	}
	policy := request.ToolPolicy()
	size += 16 + len(policy.Mode)
	if key, ok := policy.SpecificID(); ok {
		size += len(key.String())
	}
	size += 16 + len(request.ToolCallBatch().Mode)
	controls := request.Controls()
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		size += len(strconv.Itoa(value)) + 8
	}
	for _, stop := range controls.Limits.StopSequences {
		size += 8 + len(stop)
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		size += len(strconv.FormatFloat(value, 'g', -1, 64)) + 8
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		size += len(strconv.FormatFloat(value, 'g', -1, 64)) + 8
	}
	format := request.OutputFormat()
	size += 32 + len(format.Kind) + len(format.Name) + len(format.Description) + len(format.Schema.RawObject())
	if previous, ok := request.PreviousResponse(); ok {
		size += 32 + len(previous.SwobuID.String())
		if previous.Responses != nil {
			size += len(previous.Responses.ProviderResponseID.String()) + len(previous.Responses.TargetID) + 8
		}
	}
	var err error
	size, err = addItemBytes(size, request.Items())
	if err != nil {
		return 0, err
	}
	return size, nil
}

func enforceCheckpointSize(size int, limit int64) error {
	if limit <= 0 {
		return fmt.Errorf("checkpoint size limit must be positive")
	}
	if int64(size) > limit {
		return fmt.Errorf("session checkpoint exceeds maximum size of %d bytes", limit)
	}
	return nil
}

func addItemBytes(size int, items []canonical.CanonicalItem) (int, error) {
	for _, item := range items {
		size += 64
		switch item.Kind() {
		case canonical.ItemKindMessage:
			message, _ := item.Message()
			var err error
			size, err = addContentBytes(size, message.Content())
			if err != nil {
				return 0, err
			}
		case canonical.ItemKindToolCall:
			call, _ := item.ToolCall()
			size += len(call.CallID().String()) + len(call.Tool().String())
			if object, ok := call.Input().Object(); ok {
				size += len(object.Bytes())
			} else if text, ok := call.Input().Text(); ok {
				size += len(text)
			} else {
				return 0, fmt.Errorf("session checkpoint contains invalid tool input")
			}
		case canonical.ItemKindToolResult:
			result, _ := item.ToolResult()
			size += len(result.CallID().String()) + 1
			var err error
			size, err = addToolResultContentBytes(size, result.Content())
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("session checkpoint contains unsupported item kind %q", item.Kind())
		}
	}
	return size, nil
}

func addToolResultContentBytes(size int, parts []canonical.ToolResultPart) (int, error) {
	for _, part := range parts {
		size += 32
		if text, ok := part.Text(); ok {
			size += len(text.Text())
			continue
		}
		image, ok := part.Image()
		if !ok {
			return 0, fmt.Errorf("session checkpoint contains unsupported tool result content kind %q", part.Kind())
		}
		if rawURL, ok := image.Source().URL(); ok {
			size += len(rawURL.String())
			if detail, specified := image.Detail().Get(); specified {
				size += len(detail) + 1
			}
			continue
		}
		if media, ok := image.Source().Inline(); ok {
			size += len(media.MediaType()) + len(media.Data())
			if detail, specified := image.Detail().Get(); specified {
				size += len(detail) + 1
			}
			continue
		}
		return 0, fmt.Errorf("session checkpoint contains invalid tool result image source")
	}
	return size, nil
}

func addContentBytes(size int, parts []canonical.MessagePart) (int, error) {
	for _, part := range parts {
		size += 32
		if text, ok := part.Text(); ok {
			size += len(text.Text())
			continue
		}
		image, ok := part.Image()
		if !ok {
			return 0, fmt.Errorf("session checkpoint contains unsupported content kind %q", part.Kind())
		}
		if rawURL, ok := image.Source().URL(); ok {
			size += len(rawURL.String())
			if detail, specified := image.Detail().Get(); specified {
				size += len(detail) + 1
			}
			continue
		}
		if media, ok := image.Source().Inline(); ok {
			size += len(media.MediaType()) + len(media.Data())
			if detail, specified := image.Detail().Get(); specified {
				size += len(detail) + 1
			}
			continue
		}
		return 0, fmt.Errorf("session checkpoint contains invalid image source")
	}
	return size, nil
}
