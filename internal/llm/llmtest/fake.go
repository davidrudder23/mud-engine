// Package llmtest provides a scripted llm.Client for tests of code that
// *calls* Complete (the LLM command runner, talk), rather than tests of
// a provider's own Complete implementation.
package llmtest

import (
	"context"
	"sync"

	"mudengine/internal/llm"
)

// Call is one recorded Complete or Chat call. Prompt is set for
// Complete; Messages (a copy of the history passed in) for Chat.
type Call struct {
	Prompt   string
	System   string
	Messages []llm.Message
}

// FakeClient returns Reply for every call (or ReplyFunc's result, if
// set), or Err instead, if set. Records every call in Calls. For Chat,
// ReplyFunc gets the last message's text as its prompt.
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

func (f *FakeClient) Chat(_ context.Context, system string, messages []llm.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{System: system, Messages: append([]llm.Message(nil), messages...)})
	if f.Err != nil {
		return "", f.Err
	}
	if f.ReplyFunc != nil {
		prompt := ""
		if len(messages) > 0 {
			prompt = messages[len(messages)-1].Text
		}
		return f.ReplyFunc(prompt, system), nil
	}
	return f.Reply, nil
}

// LastCall returns the most recent call, and false if there's been none.
func (f *FakeClient) LastCall() (Call, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return Call{}, false
	}
	return f.Calls[len(f.Calls)-1], true
}
