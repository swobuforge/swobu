package carrier

import "testing"

func TestTransportRequestAdoptsOwnedBodyBytes(t *testing.T) {
	body := []byte("request")
	request := TransportRequest{Body: body}
	if len(request.Body) == 0 || &request.Body[0] != &body[0] {
		t.Fatal("transport request copied its owned body")
	}
}
