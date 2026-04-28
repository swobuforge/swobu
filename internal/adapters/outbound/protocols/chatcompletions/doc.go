// Package chatcompletions maps canonical conversation semantics to and from the
// chat completions wire protocol.
//
// It owns request encoding and success-stream decoding for that protocol only.
// It must not take on endpoint selection, provider wiring, or non-chat public
// contract semantics.
package chatcompletions
