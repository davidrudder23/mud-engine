// Package llmtest provides a scripted llm.Client for tests of code that
// *calls* Complete (the LLM command runner, talk), rather than tests of
// a provider's own Complete implementation.
package llmtest

import (
	"context"
	"sync"
)

type Call struct {
	Prompt string
	System string
}

// FakeClient returns Reply for every call (or ReplyFunc's result, if
// set), or Err instead, if set. Records every call in Calls.
type FakeClient struct {
	mu        sync.Mutex
	Reply     string
	ReplyFunc func(prompt, system string) string
	Err       error
	Calls     []Call
}

func (f *FakeClient) Complete(_ context.Context, prompt, system string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Prompt: prompt, System: system})
	if f.Err != nil {
		return "", f.Err
	}
	if f.ReplyFunc != nil {
		return f.ReplyFunc(prompt, system), nil
	}
	return f.Reply, nil
}
