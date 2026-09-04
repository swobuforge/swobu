package openaifamily

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMetaModelAPIComposesStandardBearerResponsesRuntime(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer commodity-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"muse-spark-1.2","object":"model"}],"object":"list"}`)
		case "/v1/responses":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["model"] != "muse-spark-1.2" || payload["stream"] != true {
				t.Fatalf("Meta Responses payload = %#v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_meta\",\"model\":\"muse-spark-1.2\",\"status\":\"in_progress\"}}\n\n"+
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n"+
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\",\"annotations\":[]}]}}\n\n"+
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_meta\",\"model\":\"muse-spark-1.2\",\"status\":\"completed\",\"output\":[]}}\n\n")
		default:
			t.Fatalf("unexpected Meta path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecMeta))
	target := provider.NewTargetSnapshot("meta", "meta", server.URL+"/v1", "env:MODEL_API_KEY", protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "muse-spark-1.2"
	probe, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil || len(probe.Options) != 1 || probe.Options[0].Name != "muse-spark-1.2" {
		t.Fatalf("Meta model discovery = %#v, %v", probe, err)
	}
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: target.ProviderDelivery})
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{Attempt: provider.AttemptContext{ExchangeID: "ex_meta"}, Canonical: request}, ingress)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err = decoded.Stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := decoded.Stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/v1/models" || paths[1] != "/v1/responses" {
		t.Fatalf("Meta request paths = %v", paths)
	}
}
