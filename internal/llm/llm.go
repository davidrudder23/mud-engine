// Package llm is the interface for calling an LLM, plus its config and
// provider implementations.
//
// Mirrors repository.PlayerRepository: game logic depends on Client,
// not on a specific provider's SDK, so the backend can change (a
// different model, a different vendor entirely) without touching
// anything that uses one. Deliberately just "send text, get text back".
package llm

import (
	"context"
	"errors"
)

var (
	// ErrRequest is wrapped by every error from a failed call to the
	// underlying LLM (bad/missing API key, network error, provider-side
	// error, etc).
	ErrRequest = errors.New("llm request failed")
	// ErrConfig is wrapped by every error from missing or invalid LLM
	// configuration (an unknown provider, a missing API key, a
	// malformed config file, etc).
	ErrConfig = errors.New("llm config error")
)

type Client interface {
	// Complete sends prompt to the model and returns its text reply.
	// system is an optional system prompt ("" for none). Returns an
	// error wrapping ErrRequest if the call fails.
	Complete(ctx context.Context, prompt, system string) (string, error)
	// Chat continues a conversation: messages is the whole history so
	// far, oldest first, alternating user and assistant and ending with
	// a user message. Returns the model's next (assistant) reply, or an
	// error wrapping ErrRequest if the call fails or the model gives no
	// text back. system is as for Complete.
	Chat(ctx context.Context, system string, messages []Message) (string, error)
}

// Role is who said a Message: the user (in the game, the player) or
// the assistant (the model -- an NPC).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of a conversation passed to Client.Chat.
type Message struct {
	Role Role
	Text string
}
