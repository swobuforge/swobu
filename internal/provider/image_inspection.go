package provider

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	_ "golang.org/x/image/webp"
)

// InspectedImage is the validated operational view of canonical image bytes.
type InspectedImage struct {
	MediaType canonical.ImageMediaType
	Bytes     []byte
	Width     int
	Height    int
}

// InspectImage applies the same byte, type, dimension, pixel, and animation
// policy to inline and fetched image carriers.
func InspectImage(declared canonical.ImageMediaType, data []byte, limits MediaLimits) (InspectedImage, error) {
	if limits.MaxImageBytes > 0 && int64(len(data)) > limits.MaxImageBytes {
		return InspectedImage{}, fmt.Errorf("media image exceeds byte limit")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return InspectedImage{}, fmt.Errorf("media bytes are not a decodable supported image: %w", err)
	}
	mediaType, err := mediaTypeForFormat(format)
	if err != nil {
		return InspectedImage{}, err
	}
	if declared != "" && declared != mediaType {
		return InspectedImage{}, fmt.Errorf("declared media type %q contradicts decoded %q", declared, mediaType)
	}
	if limits.MaxImageDimension > 0 && (config.Width > limits.MaxImageDimension || config.Height > limits.MaxImageDimension) {
		return InspectedImage{}, fmt.Errorf("media dimensions exceed limit")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width) > math.MaxInt64/int64(config.Height) {
		return InspectedImage{}, fmt.Errorf("media pixel count exceeds limit")
	}
	pixels := int64(config.Width) * int64(config.Height)
	if limits.MaxPixelsPerImage > 0 && pixels > limits.MaxPixelsPerImage {
		return InspectedImage{}, fmt.Errorf("media pixel count exceeds limit")
	}
	if mediaType == canonical.ImageMediaGIF {
		frames, err := countGIFFrames(data)
		if err != nil {
			return InspectedImage{}, fmt.Errorf("media GIF structure is invalid: %w", err)
		}
		if frames != 1 {
			return InspectedImage{}, fmt.Errorf("animated GIF images are unsupported")
		}
	}
	return InspectedImage{MediaType: mediaType, Bytes: append([]byte(nil), data...), Width: config.Width, Height: config.Height}, nil
}

func mediaTypeForFormat(format string) (canonical.ImageMediaType, error) {
	switch strings.ToLower(format) { // swobu:io-string source=provider-wire
	case "jpeg":
		return canonical.ImageMediaJPEG, nil
	case "png":
		return canonical.ImageMediaPNG, nil
	case "gif":
		return canonical.ImageMediaGIF, nil
	case "webp":
		return canonical.ImageMediaWebP, nil
	default:
		return "", fmt.Errorf("unsupported decoded image format %q", format)
	}
}

// countGIFFrames walks GIF blocks without allocating decoded pixel buffers.
func countGIFFrames(data []byte) (int, error) {
	if len(data) < 13 || string(data[:3]) != "GIF" {
		return 0, fmt.Errorf("missing header")
	}
	offset := 13
	packed := data[10]
	if packed&0x80 != 0 {
		offset += 3 * (1 << ((packed & 0x07) + 1))
	}
	frames := 0
	for offset < len(data) {
		switch data[offset] {
		case 0x3b:
			if frames == 0 {
				return 0, fmt.Errorf("contains no image frame")
			}
			return frames, nil
		case 0x21:
			offset += 2
			var err error
			offset, err = skipGIFSubBlocks(data, offset)
			if err != nil {
				return 0, err
			}
		case 0x2c:
			frames++
			if offset+10 > len(data) {
				return 0, fmt.Errorf("truncated image descriptor")
			}
			imagePacked := data[offset+9]
			offset += 10
			if imagePacked&0x80 != 0 {
				offset += 3 * (1 << ((imagePacked & 0x07) + 1))
			}
			if offset >= len(data) {
				return 0, fmt.Errorf("truncated image data")
			}
			offset++ // LZW minimum code size.
			var err error
			offset, err = skipGIFSubBlocks(data, offset)
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unknown block 0x%x at %d", data[offset], offset)
		}
	}
	return 0, fmt.Errorf("missing trailer")
}

func skipGIFSubBlocks(data []byte, offset int) (int, error) {
	for {
		if offset >= len(data) {
			return 0, fmt.Errorf("truncated sub-block")
		}
		size := int(data[offset])
		offset++
		if size == 0 {
			return offset, nil
		}
		if offset+size > len(data) {
			return 0, fmt.Errorf("truncated sub-block data")
		}
		offset += size
	}
}
