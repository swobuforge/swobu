// Package chatcompletions maps canonical exchanges to Chat Completions.
//
// It owns family-level encoding and stream decoding, not endpoint selection,
// routing, or authentication. Provider-bound projections do not become trusted
// client history. Tool names are resolved through the attempt's name mapping.
package chatcompletions
