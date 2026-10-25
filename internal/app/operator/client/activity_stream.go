package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/swobuforge/swobu/internal/routing"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// ActivityStream owns one explicitly opened observation connection. Next returns
// validated sampled now/history, never lossless events. Close releases the body;
// EOF or failure never reconnects. Next calls must not overlap.
type ActivityStream struct {
	reader    *core.SSEReaderCloser
	workspace string
	kind      string
	terminal  atomic.Bool
}

type activityStreamBody struct {
	io.Reader
	io.Closer
}

func (client *Client) OpenActivityStream(ctx context.Context, workspace, kind string) (*ActivityStream, error) {
	if kind != "" && kind != "exchange" && kind != "mcp_operation" {
		return nil, fmt.Errorf("operator client: supported Activity kind required")
	}
	if workspace != "" {
		slug, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil || slug.String() != workspace {
			return nil, fmt.Errorf("operator client: exact Activity Workspace required")
		}
	}
	query := url.Values{}
	if workspace != "" {
		query.Set("workspace", workspace)
	}
	if kind != "" {
		query.Set("kind", kind)
	}
	path := client.baseURL + "/api/v1/activity/stream"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("operator client: Activity stream request unavailable")
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("operator client: Activity stream unavailable")
	}
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaErr != nil || media != "text/event-stream" {
		response.Body.Close()
		return nil, fmt.Errorf("operator client: Activity stream unavailable or denied")
	}
	body := activityStreamBody{Reader: io.LimitReader(response.Body, 64<<20), Closer: response.Body}
	return &ActivityStream{reader: core.NewBoundedSSEReader(body, (1<<20)+32), workspace: workspace, kind: kind}, nil
}

func (stream *ActivityStream) Next(ctx context.Context) (ActivityProjection, error) {
	if stream == nil || stream.reader == nil || stream.terminal.Load() {
		return ActivityProjection{}, io.EOF
	}
	event, err := stream.reader.Next(ctx)
	if err != nil {
		stream.Close()
		if err == io.EOF {
			return ActivityProjection{}, io.EOF
		}
		return ActivityProjection{}, fmt.Errorf("operator client: Activity observation interrupted")
	}
	if event.Event != "activity" || len(event.Data) > 1<<20 {
		stream.Close()
		return ActivityProjection{}, fmt.Errorf("operator client: Activity observation unavailable or invalid")
	}
	var observation struct {
		Mode     string             `json:"mode"`
		Complete *bool              `json:"complete"`
		Activity ActivityProjection `json:"activity"`
	}
	decoder := json.NewDecoder(strings.NewReader(event.Data))
	if decoder.Decode(&observation) != nil || decoder.Decode(&struct{}{}) != io.EOF || observation.Mode != "now_history_observation" || observation.Complete == nil || *observation.Complete || observation.Activity.Page != nil {
		stream.Close()
		return ActivityProjection{}, fmt.Errorf("operator client: invalid Activity observation acknowledgement")
	}
	if err := validateActivityScope(observation.Activity, stream.workspace, stream.kind); err != nil {
		stream.Close()
		return ActivityProjection{}, err
	}
	return observation.Activity, nil
}

func (stream *ActivityStream) Close() error {
	if stream == nil || stream.reader == nil {
		return nil
	}
	stream.terminal.Store(true)
	return stream.reader.Close()
}
