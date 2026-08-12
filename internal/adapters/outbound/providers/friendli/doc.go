// Package friendli owns FriendliAI's provider-local composition around the
// shared OpenAI-family transport and standard protocol grammars.
//
// The provider deliberately keeps deployment URLs opaque: serverless,
// Dedicated, Container, and private gateway targets differ only by operator-
// authored endpoint facts. Its only codec refinement is documented Chat
// reasoning projection and readable response reasoning capture.
package friendli
