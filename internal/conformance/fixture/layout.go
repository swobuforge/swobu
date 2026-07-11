package fixture

var RequiredCases = []string{
	"chat_buffered_to_buffered",
	"chat_stream_to_stream",
	"responses_buffered_to_buffered",
	"responses_stream_to_stream",
	"messages_buffered_to_buffered",
	"provider_stream_to_client_buffered",
	"provider_buffered_to_client_stream",
}

var RequiredFiles = []string{
	"case.yaml",
	"client_request.http.json",
	"client_request.body.json",
	"canonical_request.json",
	"provider_request.body.json",
	"provider_response.body.json",
	"provider_response.sse",
	"canonical_events.jsonl",
	"client_response.body.json",
	"client_response.sse",
	"exchange_report.json",
}
