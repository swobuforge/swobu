package openaifamily

import (
	"bytes"
	"encoding/json"
	"io"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// RequestPatcher applies provider-owned patches to a protocol-encoded request.
type RequestPatcher struct{}

func (RequestPatcher) Patch(req core.WireRequest, protocol protocolkind.ProtocolKind, streaming bool, patches []core.WirePatch) (core.WireRequest, error) {
	if len(patches) == 0 || !req.HasBody {
		return req, nil
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return core.WireRequest{}, canonical.InternalError("provider request body could not be read for encode patch")
	}
	payload := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return core.WireRequest{}, canonical.InternalError("provider request body is invalid JSON for encode patch")
		}
	}
	packet := core.WirePacket{
		Kind:     core.WireKindRequest,
		Protocol: protocol,
		Method:   req.Method,
		Path:     req.Path,
		Payload:  payload,
		RawBody:  raw,
		Stream:   streaming,
	}
	for _, patch := range patches {
		if patch == nil {
			continue
		}
		if err := patch.ApplyEncode(&packet); err != nil {
			return core.WireRequest{}, canonical.InternalError("provider encode patch failed")
		}
	}
	nextRaw, err := json.Marshal(packet.Payload)
	if err != nil {
		return core.WireRequest{}, canonical.InternalError("provider request body could not be encoded after patch")
	}
	return core.WireRequest{
		Method:  req.Method,
		Path:    req.Path,
		Body:    bytes.NewReader(nextRaw),
		HasBody: true,
	}, nil
}
