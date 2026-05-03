package httpapi

import "github.com/swobuforge/swobu/internal/domain/compatibility"

type familyCodec interface {
	decodeRequest(raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error)
	encodeBuffered(output compatibility.CanonicalOutput) ([]byte, error)
	newStreamState() clientStreamEncoder
}

var familyCodecs = map[compatibility.IngressFamily]familyCodec{
	compatibility.IngressFamilyChatCompletions: chatCompletionsFamilyCodec{},
	compatibility.IngressFamilyResponses:       responsesFamilyCodec{},
	compatibility.IngressFamilyCompletions:     completionsFamilyCodec{},
	compatibility.IngressFamilyMessages:        messagesFamilyCodec{},
}

func codecForFamily(family compatibility.IngressFamily) (familyCodec, error) {
	codec, ok := familyCodecs[family]
	if !ok {
		return nil, compatibility.UnsupportedOperation("ingress family is not implemented")
	}
	return codec, nil
}
