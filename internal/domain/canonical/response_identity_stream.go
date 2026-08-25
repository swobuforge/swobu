package canonical

import "context"

// BoundResponseIdentityStream binds the exchange-allocated public identity to
// provider-decoded response identity while preserving typed native handles.
type BoundResponseIdentityStream struct {
	upstream ResponseStream
	binding  ResponseBinding
}

// ResponseBinding is the single exchange-owned public/native identity tuple.
type ResponseBinding struct {
	SwobuID       SwobuResponseID
	TargetID      string
	TargetVersion uint64
}

// NewBoundResponseIdentityStream is the sole response identity mutation point
// before validation, checkpoint commit, and projection.
func NewBoundResponseIdentityStream(upstream ResponseStream, binding ResponseBinding) *BoundResponseIdentityStream {
	return &BoundResponseIdentityStream{upstream: upstream, binding: binding}
}

func (s *BoundResponseIdentityStream) Next(ctx context.Context) (Event, error) {
	event, err := s.upstream.Next(ctx)
	if err != nil {
		return Event{}, err
	}
	if event.Kind == EventResponseIdentity {
		payload, ok := event.Payload.(ResponseIdentityPayload)
		if !ok {
			return event, nil
		}
		payload.Response.SwobuID = s.binding.SwobuID
		if payload.Response.Responses != nil {
			responses := *payload.Response.Responses
			responses.TargetID = s.binding.TargetID
			responses.TargetVersion = s.binding.TargetVersion
			payload.Response.Responses = &responses
		}
		if payload.Response.Interactions != nil {
			interactions := *payload.Response.Interactions
			interactions.TargetID = s.binding.TargetID
			interactions.TargetVersion = s.binding.TargetVersion
			payload.Response.Interactions = &interactions
		}
		event.Payload = payload
		event.Meta.NativeID = ""
	}
	if event.Kind == EventItemCompleted {
		if payload, ok := event.Payload.(ItemEvent); ok {
			if completedPayload, ok := payload.Payload.(ItemCompletedPayload); ok {
				if completedPayload.Item.Kind() == ItemKindReasoning && s.binding.TargetID != "" && s.binding.TargetVersion != 0 {
					boundItem, err := completedPayload.Item.withTargetOrigin(s.binding.TargetID, s.binding.TargetVersion)
					if err != nil {
						return Event{}, err
					}
					completedPayload.Item = boundItem
					payload.Payload = completedPayload
					event.Payload = payload
				}
			}
		}
	}
	return event, nil
}

func (s *BoundResponseIdentityStream) Close(ctx context.Context) error { return s.upstream.Close(ctx) }
