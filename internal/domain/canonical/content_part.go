package canonical

import (
	"fmt"
	"net/url"
	"strings"
)

type PartKind string

const (
	PartKindText  PartKind = "text"
	PartKindImage PartKind = "image"
)

// MessagePart is the closed content grammar legal inside a message.
type MessagePart struct {
	text      *TextPart
	image     *ImagePart
	citations []WebCitation
}

// ToolResultPart is the distinct closed content grammar legal inside a tool
// result. Shared text and image leaves do not make the parent sums fungible.
type ToolResultPart struct {
	text  *TextPart
	image *ImagePart
}

type TextPart struct{ text string }

func NewTextValue(text string) TextPart { return TextPart{text: text} }
func (p TextPart) Text() string         { return p.text }
func (p TextPart) Clone() TextPart      { return TextPart{text: p.text} }

type ImagePart struct {
	source ImageSource
	detail Specified[ImageDetail]
}
type ImageDetail string

const (
	ImageDetailLow      ImageDetail = "low"
	ImageDetailHigh     ImageDetail = "high"
	ImageDetailOriginal ImageDetail = "original"
)

type ImageSource struct {
	url    *URLImage
	inline *InlineImage
}

// URLImage is an exact, validated HTTP(S) image reference. Canonical retains
// the caller's spelling because formatting a parsed URL can alter identity.
type URLImage struct{ rawURL string }

// ImageMediaType is the closed portable inline-image media vocabulary.
type ImageMediaType string

const (
	ImageMediaJPEG ImageMediaType = "image/jpeg"
	ImageMediaPNG  ImageMediaType = "image/png"
	ImageMediaWebP ImageMediaType = "image/webp"
	ImageMediaGIF  ImageMediaType = "image/gif"
)

type InlineImage struct {
	mediaType ImageMediaType
	data      []byte
}

func NewTextMessagePart(text string) MessagePart {
	leaf := NewTextValue(text)
	return MessagePart{text: &leaf}
}

// NewCitedTextMessagePart validates citations against UTF-8 byte offsets in
// the exact emitted text.
func NewCitedTextMessagePart(text string, citations []WebCitation) (MessagePart, error) {
	out := make([]WebCitation, len(citations))
	for index, citation := range citations {
		if err := citation.validate(text); err != nil {
			return MessagePart{}, fmt.Errorf("canonical web citation %d: %w", index, err)
		}
		out[index] = citation.Clone()
	}
	leaf := NewTextValue(text)
	return MessagePart{text: &leaf, citations: out}, nil
}
func NewImageMessagePart(image ImagePart) MessagePart {
	leaf := image.Clone()
	return MessagePart{image: &leaf}
}
func NewTextToolResultPart(text string) ToolResultPart {
	leaf := NewTextValue(text)
	return ToolResultPart{text: &leaf}
}
func NewImageToolResultPart(image ImagePart) ToolResultPart {
	leaf := image.Clone()
	return ToolResultPart{image: &leaf}
}

func NewURLImage(rawURL string, detail Specified[ImageDetail]) (ImagePart, error) {
	if rawURL == "" || strings.TrimSpace(rawURL) != rawURL { // swobu:io-string source=boundary
		return ImagePart{}, fmt.Errorf("canonical image URL must be absolute HTTP(S)")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ImagePart{}, fmt.Errorf("canonical image URL must be absolute HTTP(S) without credentials")
	}
	if parsed.Fragment != "" {
		return ImagePart{}, fmt.Errorf("canonical image URL must not contain a fragment")
	}
	if err := validateImageDetail(detail); err != nil {
		return ImagePart{}, err
	}
	urlImage := URLImage{rawURL: rawURL}
	return ImagePart{source: ImageSource{url: &urlImage}, detail: cloneSpecified(detail, func(v ImageDetail) ImageDetail { return v })}, nil
}
func NewInlineImage(mediaType ImageMediaType, data []byte, detail Specified[ImageDetail]) (ImagePart, error) {
	if !portableImageMediaType(mediaType) {
		return ImagePart{}, fmt.Errorf("canonical inline image media type %q is unsupported", mediaType)
	}
	if len(data) == 0 {
		return ImagePart{}, fmt.Errorf("canonical inline image data is empty")
	}
	if err := validateImageDetail(detail); err != nil {
		return ImagePart{}, err
	}
	inline := InlineImage{mediaType: mediaType, data: append([]byte(nil), data...)}
	return ImagePart{source: ImageSource{inline: &inline}, detail: cloneSpecified(detail, func(v ImageDetail) ImageDetail { return v })}, nil
}

func (p MessagePart) Kind() PartKind {
	if p.text != nil && p.image == nil {
		return PartKindText
	}
	if p.text == nil && p.image != nil && p.image.source.valid() {
		return PartKindImage
	}
	return ""
}

// Citations returns independent trusted source annotations for a text part.
func (p MessagePart) Citations() []WebCitation {
	if p.Kind() != PartKindText || len(p.citations) == 0 {
		return nil
	}
	out := make([]WebCitation, len(p.citations))
	for index := range p.citations {
		out[index] = p.citations[index].Clone()
	}
	return out
}
func (p MessagePart) Text() (TextPart, bool) {
	if p.Kind() != PartKindText {
		return TextPart{}, false
	}
	return p.text.Clone(), true
}
func (p MessagePart) Image() (ImagePart, bool) {
	if p.Kind() != PartKindImage {
		return ImagePart{}, false
	}
	return p.image.Clone(), true
}
func (p MessagePart) Clone() MessagePart {
	if text, ok := p.Text(); ok {
		cloned, err := NewCitedTextMessagePart(text.Text(), p.citations)
		if err != nil {
			return MessagePart{}
		}
		return cloned
	}
	if image, ok := p.Image(); ok {
		return NewImageMessagePart(image)
	}
	return MessagePart{}
}

func (p ToolResultPart) Kind() PartKind {
	if p.text != nil && p.image == nil {
		return PartKindText
	}
	if p.text == nil && p.image != nil && p.image.source.valid() {
		return PartKindImage
	}
	return ""
}
func (p ToolResultPart) Text() (TextPart, bool) {
	if p.Kind() != PartKindText {
		return TextPart{}, false
	}
	return p.text.Clone(), true
}
func (p ToolResultPart) Image() (ImagePart, bool) {
	if p.Kind() != PartKindImage {
		return ImagePart{}, false
	}
	return p.image.Clone(), true
}
func (p ToolResultPart) Clone() ToolResultPart {
	if text, ok := p.Text(); ok {
		return NewTextToolResultPart(text.Text())
	}
	if image, ok := p.Image(); ok {
		return NewImageToolResultPart(image)
	}
	return ToolResultPart{}
}

func (p ImagePart) Source() ImageSource { return p.source.Clone() }
func (p ImagePart) Detail() Specified[ImageDetail] {
	return cloneSpecified(p.detail, func(v ImageDetail) ImageDetail { return v })
}
func (p ImagePart) Clone() ImagePart { return ImagePart{source: p.source.Clone(), detail: p.Detail()} }
func validateImageDetail(detail Specified[ImageDetail]) error {
	value, specified := detail.Get()
	if !specified {
		return nil
	}
	if value != ImageDetailLow && value != ImageDetailHigh && value != ImageDetailOriginal {
		return fmt.Errorf("canonical image detail %q is unsupported", value)
	}
	return nil
}
func (s ImageSource) URL() (URLImage, bool) {
	if s.url == nil || s.inline != nil {
		return URLImage{}, false
	}
	return *s.url, true
}
func (s ImageSource) Inline() (InlineImage, bool) {
	if s.inline == nil || s.url != nil {
		return InlineImage{}, false
	}
	return s.inline.Clone(), true
}
func (s ImageSource) Clone() ImageSource {
	cloned := ImageSource{}
	if s.url != nil {
		urlImage := *s.url
		cloned.url = &urlImage
	}
	if s.inline != nil {
		media := s.inline.Clone()
		cloned.inline = &media
	}
	return cloned
}
func (s ImageSource) valid() bool               { return (s.url == nil) != (s.inline == nil) }
func (u URLImage) String() string               { return u.rawURL }
func (m InlineImage) MediaType() ImageMediaType { return m.mediaType }
func (m InlineImage) Data() []byte              { return append([]byte(nil), m.data...) }
func (m InlineImage) Clone() InlineImage        { return InlineImage{mediaType: m.mediaType, data: m.Data()} }

func cloneValidatedMessageParts(parts []MessagePart) ([]MessagePart, error) {
	out := make([]MessagePart, len(parts))
	for i := range parts {
		if parts[i].Kind() == "" {
			return nil, fmt.Errorf("canonical message part %d has no valid branch", i)
		}
		out[i] = parts[i].Clone()
	}
	return out, nil
}
func cloneMessageParts(parts []MessagePart) []MessagePart {
	if parts == nil {
		return nil
	}
	out := make([]MessagePart, len(parts))
	for i := range parts {
		out[i] = parts[i].Clone()
	}
	return out
}
func cloneValidatedToolResultParts(parts []ToolResultPart) ([]ToolResultPart, error) {
	out := make([]ToolResultPart, len(parts))
	for i := range parts {
		if parts[i].Kind() == "" {
			return nil, fmt.Errorf("canonical tool result part %d has no valid branch", i)
		}
		out[i] = parts[i].Clone()
	}
	return out, nil
}
func cloneToolResultParts(parts []ToolResultPart) []ToolResultPart {
	if parts == nil {
		return nil
	}
	out := make([]ToolResultPart, len(parts))
	for i := range parts {
		out[i] = parts[i].Clone()
	}
	return out
}
func portableImageMediaType(mediaType ImageMediaType) bool {
	switch mediaType {
	case ImageMediaJPEG, ImageMediaPNG, ImageMediaGIF, ImageMediaWebP:
		return true
	default:
		return false
	}
}
