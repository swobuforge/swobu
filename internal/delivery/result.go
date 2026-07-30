package delivery

// ResultKind classifies the terminal outcome owned by the inbound response
// delivery adapter.
type ResultKind string

const (
	Succeeded              ResultKind = "succeeded"
	ClientCancelled        ResultKind = "client_cancelled"
	ClientWriteFailed      ResultKind = "client_write_failed"
	ProviderStreamFailed   ResultKind = "provider_stream_failed"
	CheckpointCommitFailed ResultKind = "checkpoint_commit_failed"
	ExchangeFailed         ResultKind = "exchange_failed"
)

// Result is the concrete terminal outcome of consuming and writing one client
// response.
type Result struct {
	Kind ResultKind
	Err  error
}

// ValidResultKind reports whether kind is a recognized terminal outcome.
func ValidResultKind(kind ResultKind) bool {
	switch kind {
	case Succeeded, ClientCancelled, ClientWriteFailed, ProviderStreamFailed, CheckpointCommitFailed, ExchangeFailed:
		return true
	}
	return false
}
