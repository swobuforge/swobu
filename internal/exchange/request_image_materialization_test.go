package exchange

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire/messages"
)

type fixedImageFetcher struct {
	fetched provider.FetchedImageResult
	calls   int
}

type failingImageFetcher struct{ err error }

func captureMaterializationDebugLogs() (func(), *bytes.Buffer) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(previous) }, &logs
}

func (f failingImageFetcher) FetchImage(context.Context, canonical.URLImage, provider.NetworkPolicy, int64) (provider.FetchedImageResult, error) {
	return provider.FetchedImageResult{}, f.err
}

func (f *fixedImageFetcher) FetchImage(context.Context, canonical.URLImage, provider.NetworkPolicy, int64) (provider.FetchedImageResult, error) {
	f.calls++
	return f.fetched, nil
}

func TestMaterializeRequestImagesReusesResolvedBytesAcrossCandidates(t *testing.T) {
	urlImage, _ := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(urlImage)})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}})
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	fetcher := &fixedImageFetcher{fetched: provider.FetchedImageResult{Bytes: data.Bytes(), DeclaredMediaType: canonical.ImageMediaPNG}}
	_, cache, _, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, nil, session.ResolvedMedia{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, cache, session.ResolvedMedia{})
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want one exchange resolution", fetcher.calls)
	}
}

func TestMaterializeRequestImagesReturnsTypedBadRequestForFetchFailure(t *testing.T) {
	urlImage, _ := canonical.NewURLImage("https://example.test/image.png?token=secret", canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(urlImage)})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}})
	restore, logs := captureMaterializationDebugLogs()
	defer restore()
	_, _, _, err := materializeRequestImages(
		context.Background(),
		request,
		provider.DefaultImageFetchPolicy(),
		provider.DefaultMediaLimits(),
		failingImageFetcher{err: errors.New(`Get "https://example.test/image.png?token=secret": connection reset`)},
		nil,
		session.ResolvedMedia{},
	)
	var typed canonical.Error
	if !errors.As(err, &typed) || typed.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("fetch error = %T %v, want typed bad request", err, err)
	}
	if strings.Contains(err.Error(), "token=secret") || strings.Contains(logs.String(), "token=secret") {
		t.Fatalf("materialization exposed sensitive URL\nerror: %v\nlogs:\n%s", err, logs.String())
	}
	if !strings.Contains(logs.String(), "source_host=example.test") {
		t.Fatalf("sanitized diagnostics missing source host:\n%s", logs.String())
	}
}

func TestMaterializeRequestImagesPreservesCancellation(t *testing.T) {
	request, _ := testURLImageRequest(t, "https://example.test/image.png")
	_, _, _, err := materializeRequestImages(
		context.Background(),
		request,
		provider.DefaultImageFetchPolicy(),
		provider.DefaultMediaLimits(),
		failingImageFetcher{err: context.Canceled},
		nil,
		session.ResolvedMedia{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("materialization cancellation = %T %v", err, err)
	}
}

func TestMaterializeRequestImagesClassifiesCorruptStoredMediaAsInternal(t *testing.T) {
	request, _ := testURLImageRequest(t, "https://example.test/history.png")
	historical, err := (session.ResolvedMedia{}).Bind(
		canonical.RequestPartRef{},
		"https://example.test/history.png",
		canonical.ImageMediaPNG,
		[]byte("not-a-png"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = materializeRequestImages(
		context.Background(),
		request,
		provider.DefaultImageFetchPolicy(),
		provider.DefaultMediaLimits(),
		nil,
		nil,
		historical,
	)
	var typed canonical.Error
	if !errors.As(err, &typed) || typed.Code != canonical.ErrorCodeInternal {
		t.Fatalf("stored-media error = %T %v, want typed internal", err, err)
	}
}

func TestMaterializeRequestImagesClassifiesPolicyRuntimeAndContentFailures(t *testing.T) {
	request, _ := testURLImageRequest(t, "https://example.test/image.png")
	invalidFetcher := &fixedImageFetcher{fetched: provider.FetchedImageResult{DeclaredMediaType: canonical.ImageMediaPNG, Bytes: []byte("not-a-png")}}
	for name, test := range map[string]struct {
		policy  provider.ImageFetchPolicy
		fetcher provider.ImageFetcher
	}{
		"disabled":        {policy: provider.DisabledImageFetchPolicy()},
		"missing fetcher": {policy: provider.DefaultImageFetchPolicy()},
		"invalid bytes":   {policy: provider.DefaultImageFetchPolicy(), fetcher: invalidFetcher},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := materializeRequestImages(context.Background(), request, test.policy, provider.DefaultMediaLimits(), test.fetcher, nil, session.ResolvedMedia{})
			var typed canonical.Error
			if !errors.As(err, &typed) {
				t.Fatalf("materialization error = %T %v, want canonical error", err, err)
			}
			want := canonical.ErrorCodeBadRequest
			if name == "disabled" {
				want = canonical.ErrorCodeNotImplemented
			}
			if name == "missing fetcher" {
				want = canonical.ErrorCodeInternal
			}
			if typed.Code != want {
				t.Fatalf("materialization error code = %q, want %q: %v", typed.Code, want, err)
			}
		})
	}
}

func TestMaterializeRequestImagesHistoricalBindingPreventsURLRefetch(t *testing.T) {
	request, imageBytes := testURLImageRequest(t, "https://example.test/history.png")
	historical, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/history.png", canonical.ImageMediaPNG, imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	prepared, _, _, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), failingImageFetcher{err: context.DeadlineExceeded}, nil, historical)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := prepared.Items()[0].Message()
	image, _ := message.Content()[0].Image()
	inline, ok := image.Source().Inline()
	if !ok || !bytes.Equal(inline.Data(), imageBytes) {
		t.Fatal("historical materialized bytes were not reused")
	}
}

func TestMaterializeRequestImagesSameURLAtNewOccurrenceFetchesCurrentAsset(t *testing.T) {
	first, bytesA := testURLImageRequest(t, "https://example.test/current.png")
	secondMessage := first.Items()[0]
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{first.Items()[0], secondMessage}})
	historical, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{Item: 0}, "https://example.test/current.png", canonical.ImageMediaPNG, bytesA)
	if err != nil {
		t.Fatal(err)
	}
	var bytesB bytes.Buffer
	imageB := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	imageB.Set(1, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&bytesB, imageB); err != nil {
		t.Fatal(err)
	}
	fetcher := &fixedImageFetcher{fetched: provider.FetchedImageResult{DeclaredMediaType: canonical.ImageMediaPNG, Bytes: bytesB.Bytes()}}
	prepared, _, used, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, nil, historical)
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 || used.BindingCount() != 2 || used.AssetCount() != 2 {
		t.Fatalf("fetches=%d media=%#v", fetcher.calls, used)
	}
	firstPrepared, _ := prepared.Items()[0].Message()
	secondPrepared, _ := prepared.Items()[1].Message()
	firstImage, _ := firstPrepared.Content()[0].Image()
	secondImage, _ := secondPrepared.Content()[0].Image()
	firstInline, _ := firstImage.Source().Inline()
	secondInline, _ := secondImage.Source().Inline()
	if !bytes.Equal(firstInline.Data(), bytesA) || !bytes.Equal(secondInline.Data(), bytesB.Bytes()) {
		t.Fatal("same URL occurrences did not retain distinct historical/current assets")
	}
}

func TestNativeResumptionDoesNotApplyFullHistoryMediaCoordinates(t *testing.T) {
	request, imageBytes := testURLImageRequest(t, "https://example.test/current.png")
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: request.Items(),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_1", Responses: &canonical.ResponsesContinuation{
			ProviderResponseID: "provider_1", TargetID: "target", TargetVersion: 1,
		}},
	})
	historical, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/current.png", canonical.ImageMediaPNG, imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := historicalMediaForAttempt(request, historical); got.BindingCount() != 0 || got.AssetCount() != 0 {
		t.Fatalf("native delta inherited full-history coordinates: %#v", got)
	}
}

func TestNativeResumptionMediaBindingsRebaseToSemanticSuffix(t *testing.T) {
	previous, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("previous")})
	current, imageBytes := testURLImageRequest(t, "https://example.test/current.png")
	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{previous, current.Items()[0]}})
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Items: current.Items()})
	prepared := session.ResolvedRequest{Full: semantic, Delta: delta}
	used, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/current.png", canonical.ImageMediaPNG, imageBytes)
	if err != nil {
		t.Fatal(err)
	}

	rebased, err := rebaseAttemptMedia(prepared, delta, used)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rebased.Resolve(canonical.RequestPartRef{Item: 1}, "https://example.test/current.png"); !ok {
		t.Fatal("native delta media binding was not shifted to semantic item 1")
	}
	if err := rebased.ValidateForRequest(semantic); err != nil {
		t.Fatalf("rebased media does not validate against semantic request: %v", err)
	}
}

func TestNativeResumptionMediaRebasePreservesExistingHistoryAndPartIndexes(t *testing.T) {
	previous, previousBytes := testURLImageRequest(t, "https://example.test/previous.png")
	currentURL, currentBytes := testURLImageRequest(t, "https://example.test/current.png")
	currentMessage, _ := currentURL.Items()[0].Message()
	currentImage, _ := currentMessage.Content()[0].Image()
	current, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("current"),
		canonical.NewImageMessagePart(currentImage),
		canonical.NewImageMessagePart(currentImage),
	})
	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{previous.Items()[0], current}})
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{current}})
	prepared := session.ResolvedRequest{Full: semantic, Delta: delta}
	historical, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/previous.png", canonical.ImageMediaPNG, previousBytes)
	if err != nil {
		t.Fatal(err)
	}
	used, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{Part: 1}, "https://example.test/current.png", canonical.ImageMediaPNG, currentBytes)
	if err != nil {
		t.Fatal(err)
	}
	used, err = used.Bind(canonical.RequestPartRef{Part: 2}, "https://example.test/current.png", canonical.ImageMediaPNG, currentBytes)
	if err != nil {
		t.Fatal(err)
	}
	rebased, err := rebaseAttemptMedia(prepared, delta, used)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := historical.Merge(rebased)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged.Resolve(canonical.RequestPartRef{}, "https://example.test/previous.png"); !ok {
		t.Fatal("historical item zero binding was lost")
	}
	if _, ok := merged.Resolve(canonical.RequestPartRef{Item: 1, Part: 1}, "https://example.test/current.png"); !ok {
		t.Fatal("current image part index was not preserved while shifting item index")
	}
	if _, ok := merged.Resolve(canonical.RequestPartRef{Item: 1, Part: 2}, "https://example.test/current.png"); !ok {
		t.Fatal("second current image part index was not preserved while shifting item index")
	}
	if err := merged.ValidateForRequest(semantic); err != nil {
		t.Fatal(err)
	}
}

func TestNativeResumptionMediaRebaseRejectsNonSuffixDelta(t *testing.T) {
	semanticItem, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("semantic")})
	deltaItem, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("different")})
	prepared := session.ResolvedRequest{
		Full:  canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{semanticItem}}),
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{deltaItem}}),
	}
	if _, err := rebaseAttemptMedia(prepared, prepared.Delta, session.ResolvedMedia{}); err == nil {
		t.Fatal("non-suffix native delta was accepted")
	}
}

func TestFullHistoryMediaBindingsKeepSemanticPositions(t *testing.T) {
	request, imageBytes := testURLImageRequest(t, "https://example.test/full.png")
	prepared := session.ResolvedRequest{Full: request, Delta: canonical.NewCanonicalRequest(canonical.RequestParams{})}
	used, err := (session.ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/full.png", canonical.ImageMediaPNG, imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	rebased, err := rebaseAttemptMedia(prepared, request, used)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rebased.Resolve(canonical.RequestPartRef{}, "https://example.test/full.png"); !ok {
		t.Fatal("full-history binding position changed")
	}
}

func TestWinningAttemptReusesAndBindsPreviouslyFetchedAsset(t *testing.T) {
	request, imageBytes := testURLImageRequest(t, "https://example.test/winner.png")
	asset, err := provider.InspectImage(canonical.ImageMediaPNG, imageBytes, provider.DefaultMediaLimits())
	if err != nil {
		t.Fatal(err)
	}
	loserCache := mediaFetchCache{"https://example.test/winner.png": asset}
	prepared, _, used, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), nil, loserCache, session.ResolvedMedia{})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := prepared.Items()[0].Message()
	image, _ := message.Content()[0].Image()
	inline, ok := image.Source().Inline()
	if !ok || !bytes.Equal(inline.Data(), imageBytes) {
		t.Fatal("winning attempt did not use the exchange-fetched immutable bytes")
	}
	if used.BindingCount() != 1 || used.AssetCount() != 1 {
		t.Fatalf("winning attempt did not bind the bytes it used: %#v", used)
	}
}

func testURLImageRequest(t *testing.T, rawURL string) (canonical.CanonicalRequest, []byte) {
	t.Helper()
	urlImage, err := canonical.NewURLImage(rawURL, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(urlImage)})
	if err != nil {
		t.Fatal(err)
	}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}}), imageBytes.Bytes()
}

func TestMaterializeRequestImagesRejectsInvalidInlineBytesAsBadRequest(t *testing.T) {
	inline, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte("not-a-png"), canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(inline)})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}})
	_, _, _, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), nil, nil, session.ResolvedMedia{})
	var typed canonical.Error
	if !errors.As(err, &typed) || typed.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("inline error = %T %v, want typed bad request", err, err)
	}
}

func TestMaterializeRequestImagesKeepsSourceRequestAndProducesInlineCarrier(t *testing.T) {
	urlImage, err := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(urlImage)})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{message}})
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	fetcher := &fixedImageFetcher{fetched: provider.FetchedImageResult{DeclaredMediaType: canonical.ImageMediaPNG, Bytes: imageBytes.Bytes()}}

	prepared, _, resolved, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, nil, session.ResolvedMedia{})
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
	originalImage, _ := request.Items()[0].Message()
	if _, ok := originalImage.Content()[0].Image(); !ok {
		t.Fatal("source request image disappeared")
	}
	originalPart, _ := originalImage.Content()[0].Image()
	if _, ok := originalPart.Source().URL(); !ok {
		t.Fatal("source request URL carrier was mutated")
	}
	preparedMessage, _ := prepared.Items()[0].Message()
	preparedPart, _ := preparedMessage.Content()[0].Image()
	inline, ok := preparedPart.Source().Inline()
	if !ok || !bytes.Equal(inline.Data(), imageBytes.Bytes()) {
		t.Fatalf("prepared carrier = %#v, want fetched inline bytes", preparedPart.Source())
	}
	if resolved.BindingCount() != 1 || resolved.AssetCount() != 1 {
		t.Fatalf("resolved media = %#v", resolved)
	}
	document, err := messages.EncodeCarrierWithChanges(prepared, nil, delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatalf("Bedrock-compatible Messages encoding rejected prepared URL image: %v", err)
	}
	body := string(document.RawBytes())
	if !strings.Contains(body, `"type":"base64"`) || strings.Contains(body, "https://example.test/image.png") {
		t.Fatalf("Bedrock-compatible Messages carrier was not inline: %s", body)
	}
}

func TestMaterializeRequestImagesPreservesToolResultImagePlacementWithoutProtocolInput(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	image, err := canonical.NewURLImage("https://example.test/tool.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewImageToolResultPart(image)}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{result}})
	fetcher := &fixedImageFetcher{fetched: provider.FetchedImageResult{
		DeclaredMediaType: canonical.ImageMediaPNG,
		Bytes:             imageBytes.Bytes(),
	}}
	materialized, _, resolved, err := materializeRequestImages(context.Background(), request, provider.DefaultImageFetchPolicy(), provider.DefaultMediaLimits(), fetcher, nil, session.ResolvedMedia{})
	if err != nil {
		t.Fatal(err)
	}
	gotResult, ok := materialized.Items()[0].ToolResult()
	gotImage, imageOK := gotResult.Content()[0].Image()
	inline, inlineOK := gotImage.Source().Inline()
	if !ok || !imageOK || !inlineOK || !bytes.Equal(inline.Data(), imageBytes.Bytes()) {
		t.Fatalf("materialized item = %#v, want tool-result image", materialized.Items()[0])
	}
	if fetcher.calls != 1 || resolved.BindingCount() != 1 {
		t.Fatalf("materialization provenance = fetches %d resolved %#v", fetcher.calls, resolved)
	}
	if _, exists := resolved.Resolve(canonical.RequestPartRef{Item: 0, Part: 0}, "https://example.test/tool.png"); !exists {
		t.Fatal("durable binding lost the canonical tool-result coordinate")
	}
}
