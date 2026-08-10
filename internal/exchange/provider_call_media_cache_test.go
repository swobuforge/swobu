package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type mediaEncodingMode uint8

const (
	mediaURLNative mediaEncodingMode = iota
	mediaByteOnly
)

type mediaCacheTestCodec struct {
	mode     mediaEncodingMode
	resolved *[][]byte
}

func (c mediaCacheTestCodec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	if c.mode == mediaURLNative {
		return carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}), nil, nil
	}
	var encoded []byte
	err := canonical.WalkRequestImages(request.Canonical, func(_ canonical.RequestPartRef, _ canonical.ImagePlacement, image canonical.ImagePart) error {
		urlImage, ok := image.Source().URL()
		if !ok {
			return nil
		}
		asset, err := request.EncodeContext.ResolveImage(request.EncodeContext.Context, urlImage)
		if err != nil {
			return err
		}
		encoded = append([]byte(nil), asset.Bytes...)
		return nil
	})
	if err != nil {
		return carrier.Document{}, nil, err
	}
	*c.resolved = append(*c.resolved, encoded)
	return carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}), nil, nil
}

func (mediaCacheTestCodec) Decode(context.Context, provider.Request, provider.Ingress) (provider.DecodedResponse, error) {
	panic("media cache preparation tests do not decode provider responses")
}

type mediaCacheTestRuntime struct {
	testRuntimeResolver
	codecs map[string]provider.Codec
}

func (r mediaCacheTestRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	codec := r.codecs[target.TargetID]
	return provider.Backend{
		Target: target, Codec: codec,
		Transport: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			panic("media cache preparation tests do not send provider requests")
		}),
	}, nil
}

type scriptedImageFetcher struct {
	calls   int
	results []error
}

func (f *scriptedImageFetcher) FetchImage(context.Context, canonical.URLImage, provider.NetworkPolicy, int64) (provider.FetchedImageResult, error) {
	f.calls++
	if len(f.results) > 0 {
		err := f.results[0]
		f.results = f.results[1:]
		if err != nil {
			return provider.FetchedImageResult{}, err
		}
	}
	return provider.FetchedImageResult{DeclaredMediaType: canonical.ImageMediaPNG, Bytes: append([]byte(nil), onePixelPNG...)}, nil
}

func TestProviderPreparationCarriesMediaCacheAcrossAttempts(t *testing.T) {
	var firstBytes [][]byte
	var secondBytes [][]byte
	state, runner := mediaCachePreparationFixture(t, []mediaEncodingMode{mediaByteOnly, mediaByteOnly}, &firstBytes, &secondBytes)
	fetcher := &scriptedImageFetcher{}
	runner.ImageFetcher = fetcher

	state = prepareMediaCacheAttempt(t, state, runner, 0)
	state = prepareMediaCacheAttempt(t, state, runner, 1)

	if fetcher.calls != 1 {
		t.Fatalf("image fetch calls = %d, want 1", fetcher.calls)
	}
	if len(firstBytes) != 1 || len(secondBytes) != 1 || string(firstBytes[0]) != string(secondBytes[0]) {
		t.Fatalf("encoded bytes differ: first=%d second=%d", len(firstBytes), len(secondBytes))
	}
}

func TestProviderPreparationDefersFetchUntilByteOnlyCandidate(t *testing.T) {
	var nativeBytes [][]byte
	var byteOnlyBytes [][]byte
	state, runner := mediaCachePreparationFixture(t, []mediaEncodingMode{mediaURLNative, mediaByteOnly}, &nativeBytes, &byteOnlyBytes)
	fetcher := &scriptedImageFetcher{}
	runner.ImageFetcher = fetcher

	state = prepareMediaCacheAttempt(t, state, runner, 0)
	if fetcher.calls != 0 {
		t.Fatalf("URL-native preparation fetched %d images", fetcher.calls)
	}
	_ = prepareMediaCacheAttempt(t, state, runner, 1)
	if fetcher.calls != 1 || len(byteOnlyBytes) != 1 {
		t.Fatalf("byte-only preparation fetches=%d encodes=%d, want 1/1", fetcher.calls, len(byteOnlyBytes))
	}
}

func TestProviderPreparationDoesNotCacheFetchFailure(t *testing.T) {
	var firstBytes [][]byte
	var secondBytes [][]byte
	state, runner := mediaCachePreparationFixture(t, []mediaEncodingMode{mediaByteOnly, mediaByteOnly}, &firstBytes, &secondBytes)
	fetcher := &scriptedImageFetcher{results: []error{errors.New("temporary fetch failure"), nil}}
	runner.ImageFetcher = fetcher

	_, _, _, cache, err := prepareProviderCall(context.Background(), state, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestFullHistory}, runner)
	if err == nil {
		t.Fatal("first preparation unexpectedly succeeded")
	}
	state.mediaFetchCache = cloneMediaFetchCache(cache)
	_ = prepareMediaCacheAttempt(t, state, runner, 1)
	if fetcher.calls != 2 || len(secondBytes) != 1 {
		t.Fatalf("retry fetches=%d encodes=%d, want 2/1", fetcher.calls, len(secondBytes))
	}
}

func TestProviderPreparationMediaCacheIsExchangeScoped(t *testing.T) {
	var firstBytes [][]byte
	var secondBytes [][]byte
	first, firstRunner := mediaCachePreparationFixture(t, []mediaEncodingMode{mediaByteOnly}, &firstBytes)
	second, secondRunner := mediaCachePreparationFixture(t, []mediaEncodingMode{mediaByteOnly}, &secondBytes)
	fetcher := &scriptedImageFetcher{}
	firstRunner.ImageFetcher = fetcher
	secondRunner.ImageFetcher = fetcher

	_ = prepareMediaCacheAttempt(t, first, firstRunner, 0)
	_ = prepareMediaCacheAttempt(t, second, secondRunner, 0)
	if fetcher.calls != 2 {
		t.Fatalf("independent exchange fetches = %d, want 2", fetcher.calls)
	}
}

func mediaCachePreparationFixture(t *testing.T, modes []mediaEncodingMode, resolved ...*[][]byte) (exchangeState, runtimeBundle) {
	t.Helper()
	request := requestWithURLImage(t, "https://example.test/image.png")
	prepared := mustBeginSession(t, request)
	state := reducerTestState(t)
	state.input.request = request
	state.prepared = &prepared
	state.route = routePlan{targets: make([]routing.Target, len(modes))}
	codecs := make(map[string]provider.Codec, len(modes))
	for index, mode := range modes {
		id := "media-" + string(rune('a'+index))
		state.route.targets[index] = requestpathTarget(t, id)
		path, err := resolveProviderPath(state.route.targets[index])
		if err != nil {
			t.Fatal(err)
		}
		codecs[path.target.TargetID] = mediaCacheTestCodec{mode: mode, resolved: resolved[index]}
	}
	runner := reducerRuntime()
	runner.Runtime = mediaCacheTestRuntime{codecs: codecs}
	return state, runner
}

func prepareMediaCacheAttempt(t *testing.T, state exchangeState, runner runtimeBundle, candidate int) exchangeState {
	t.Helper()
	_, _, _, cache, err := prepareProviderCall(context.Background(), state, providerCallSelection{candidateIndex: candidate, requestChoice: providerRequestFullHistory}, runner)
	if err != nil {
		t.Fatal(err)
	}
	state.mediaFetchCache = cloneMediaFetchCache(cache)
	return state
}
