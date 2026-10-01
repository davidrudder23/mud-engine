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
}
