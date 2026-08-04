package exchange

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestImageResolverReusesExchangeCache(t *testing.T) {
	fetcher := &countingImageFetcher{result: provider.FetchedImageResult{DeclaredMediaType: canonical.ImageMediaPNG, Bytes: onePixelPNG}}
	cache := mediaFetchCache{}
	resolve := newImageResolver(provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, &cache)
	urlImage := mustURLImage(t, "https://example.test/image.png")
	first, err := resolve(context.Background(), urlImage)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolve(context.Background(), urlImage)
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 || len(cache) != 1 {
		t.Fatalf("resolver calls=%d cache=%d, want 1/1", fetcher.calls, len(cache))
	}
	first.Bytes[0] = 0
	if second.Bytes[0] == 0 || cache[urlImage.String()].Bytes[0] == 0 {
		t.Fatal("resolver returned mutable cache storage")
	}
}

func TestURLNativeProviderPreparationDoesNotCallImageResolver(t *testing.T) {
	request := requestWithURLImage(t, "https://example.test/image.png")
	calls := 0
	document, _, err := (protocolcodec.Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{
		Canonical: request,
		Delivery:  delivery.BufferedDelivery(),
		EncodeContext: provider.EncodeContext{Context: context.Background(), ResolveImage: func(context.Context, canonical.URLImage) (provider.InspectedImage, error) {
			calls++
			return provider.InspectedImage{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("URL-native Responses codec called resolver %d times", calls)
	}
	if !strings.Contains(string(document.RawBytes()), "https://example.test/image.png") {
		t.Fatalf("Responses codec did not preserve URL: %s", document.RawBytes())
	}
}

var onePixelPNG = []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}

type countingImageFetcher struct {
	calls  int
	result provider.FetchedImageResult
}

func (f *countingImageFetcher) FetchImage(context.Context, canonical.URLImage, provider.NetworkPolicy, int64) (provider.FetchedImageResult, error) {
	f.calls++
	return f.result, nil
}

type fixedImageFetcher struct{ fetched provider.FetchedImageResult }

func (f *fixedImageFetcher) FetchImage(context.Context, canonical.URLImage, provider.NetworkPolicy, int64) (provider.FetchedImageResult, error) {
	return f.fetched, nil
}

func testURLImageRequest(t *testing.T, rawURL string) (canonical.CanonicalRequest, []byte) {
	t.Helper()
	return requestWithURLImage(t, rawURL), append([]byte(nil), onePixelPNG...)
}

func requestWithURLImage(t *testing.T, rawURL string) canonical.CanonicalRequest {
	t.Helper()
	image, err := canonical.NewURLImage(rawURL, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})
}

func mustURLImage(t *testing.T, rawURL string) canonical.URLImage {
	t.Helper()
	image, err := canonical.NewURLImage(rawURL, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	urlImage, ok := image.Source().URL()
	if !ok {
		t.Fatal("URL image did not retain URL source")
	}
	return urlImage
}
