package canonical

import "fmt"

// ImagePlacement identifies the parent grammar containing an image part.
type ImagePlacement string

const (
	ImageInMessage    ImagePlacement = "message"
	ImageInToolResult ImagePlacement = "tool_result"
)

// RequestPartRef identifies one stable item/part occurrence in a
// materialized canonical request. It is not a progressive stream coordinate.
type RequestPartRef struct {
	Item uint32
	Part uint32
}

// WalkRequestImages visits every image in semantic item and part order.
func WalkRequestImages(request CanonicalRequest, visit func(RequestPartRef, ImagePlacement, ImagePart) error) error {
	for itemIndex, item := range request.Items() {
		if message, ok := item.Message(); ok {
			for partIndex, part := range message.Content() {
				if image, ok := part.Image(); ok {
					if err := visit(RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)}, ImageInMessage, image); err != nil {
						return err
					}
				}
			}
		}
		if result, ok := item.ToolResult(); ok {
			if _, search := result.WebSearch(); search {
				continue
			}
			for partIndex, part := range result.Content() {
				if image, ok := part.Image(); ok {
					if err := visit(RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)}, ImageInToolResult, image); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// RewriteRequestImages clones a request and replaces only its image leaves.
func RewriteRequestImages(request CanonicalRequest, rewrite func(RequestPartRef, ImagePlacement, ImagePart) (ImagePart, error)) (CanonicalRequest, error) {
	items := request.Items()
	for itemIndex, item := range items {
		if message, ok := item.Message(); ok {
			parts := message.Content()
			for partIndex, part := range parts {
				image, ok := part.Image()
				if !ok {
					continue
				}
				prepared, err := rewrite(RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)}, ImageInMessage, image)
				if err != nil {
					return CanonicalRequest{}, err
				}
				parts[partIndex] = NewImageMessagePart(prepared)
			}
			prepared, err := NewMessageItem(message.Role(), parts)
			if err != nil {
				return CanonicalRequest{}, err
			}
			items[itemIndex] = prepared
			continue
		}
		if result, ok := item.ToolResult(); ok {
			if search, ok := result.WebSearch(); ok {
				prepared, err := NewWebSearchResultItem(result.CallID(), search)
				if err != nil {
					return CanonicalRequest{}, err
				}
				items[itemIndex] = prepared
				continue
			}
			parts := result.Content()
			for partIndex, part := range parts {
				image, ok := part.Image()
				if !ok {
					continue
				}
				prepared, err := rewrite(RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)}, ImageInToolResult, image)
				if err != nil {
					return CanonicalRequest{}, err
				}
				parts[partIndex] = NewImageToolResultPart(prepared)
			}
			prepared, err := NewToolResultItem(result.CallID(), parts, result.IsError())
			if err != nil {
				return CanonicalRequest{}, err
			}
			items[itemIndex] = prepared
		}
	}
	if len(items) != len(request.Items()) {
		return CanonicalRequest{}, fmt.Errorf("canonical image rewrite changed item topology")
	}
	return requestWithRewrittenItems(request, items), nil
}

func requestWithRewrittenItems(request CanonicalRequest, items []CanonicalItem) CanonicalRequest {
	params := RequestParams{
		Model: request.ModelField(), Items: items,
		ToolPolicy: request.ToolPolicyField(), ToolCallBatch: request.ToolCallBatchField(),
		Controls: request.Controls(), Reasoning: request.Reasoning(), OutputFormat: request.OutputFormatField(),
		Store: request.StoreField(),
	}
	if previous, ok := request.PreviousResponse(); ok {
		params.PreviousResponse = &previous
	}
	return NewCanonicalRequest(params)
}
