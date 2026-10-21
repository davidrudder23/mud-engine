package commands

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"mudengine/internal/llm"
	"mudengine/internal/llm/llmtest"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

// chatFixture is talk and say sharing one NPC conversation, as BuildAll
// wires them, over a fake LLM, with Old Tom standing in the start room.
func chatFixture(t *testing.T) (*fixture, *llmtest.FakeClient, *TalkCommand, *SayCommand, *world.NPC) {
	f := newFixture(t)
	tom := f.addTom("A grizzled bartender.")
	fake := &llmtest.FakeClient{Reply: "Welcome, traveler."}
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) { return fake, nil })
	say := NewSayCommand(f.sessions)
	say.chat = talk.chat
	return f, fake, talk, say, tom
}

func TestSayWithoutAConversationAsksNoOne(t *testing.T) {
	f, fake, _, say, _ := chatFixture(t)
	if out := f.run(t, say, "say hello"); out != "You say: hello" {
		t.Errorf("got %q", out)
	}
	if len(fake.Calls) != 0 {
		t.Errorf("LLM called: %v", fake.Calls)
	}
}

func TestConversation(t *testing.T) {
	f, fake, talk, say, tom := chatFixture(t)
	bob := f.join(t, "bob", worldtest.StartID)

	if out := f.run(t, talk, "talk to tom"); out != tomGreeting {
		t.Fatalf("talk: %q", out)
	}
	if got := received(bob); !slices.Equal(got, []string{"Old Tom says to alice: Welcome, traveler."}) {
		t.Errorf("bob heard %q", got)
	}

	fake.Reply = "Ale's a dollar."
	if out := f.run(t, say, "say What's good here?"); out != "You say: What's good here?\r\nOld Tom says: Ale's a dollar." {
		t.Errorf("say: %q", out)
	}
	if got := received(bob); !slices.Equal(got, []string{"alice says: What's good here?", "Old Tom says to alice: Ale's a dollar."}) {
		t.Errorf("bob heard %q", got)
	}

	// The NPC hears the whole conversation so far, every time.
	call, _ := fake.LastCall()
	want := []llm.Message{
		{Role: llm.RoleUser, Text: "[alice walks up to you.]"},
		{Role: llm.RoleAssistant, Text: "Welcome, traveler."},
		{Role: llm.RoleUser, Text: "What's good here?"},
	}
	if !slices.Equal(call.Messages, want) {
		t.Errorf("history sent = %+v", call.Messages)
	}
	if conversation := f.session.Conversation; conversation == nil || conversation.NPC != tom || len(conversation.History) != 4 {
		t.Errorf("conversation = %+v", conversation)
	}
}

func TestConversationEndsWhenThePlayerLeaves(t *testing.T) {
	f, fake, talk, say, _ := chatFixture(t)
	f.run(t, talk, "talk tom")

	if err := f.world.MovePlayer(f.player, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	calls := len(fake.Calls)
	if out := f.run(t, say, "say hello?"); out != "You say: hello?" {
		t.Errorf("got %q", out)
	}
	// Coming back doesn't pick it up again.
	if err := f.world.MovePlayer(f.player, worldtest.StartID); err != nil {
		t.Fatal(err)
	}
	if out := f.run(t, say, "say hello?"); out != "You say: hello?" {
		t.Errorf("back again: %q", out)
	}
	if len(fake.Calls) != calls || f.session.Conversation != nil {
		t.Errorf("conversation still going: %+v", f.session.Conversation)
	}
}

func TestConversationEndsWhenTheNPCLeaves(t *testing.T) {
	f, fake, talk, say, tom := chatFixture(t)
	f.run(t, talk, "talk tom")
	if err := f.world.MoveNPC(tom, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	calls := len(fake.Calls)
	if out := f.run(t, say, "say hello?"); out != "You say: hello?" {
		t.Errorf("got %q", out)
	}
	if len(fake.Calls) != calls || f.session.Conversation != nil {
		t.Errorf("conversation still going: %+v", f.session.Conversation)
	}
}

func TestTalkingAgainStartsOver(t *testing.T) {
	f, fake, talk, say, _ := chatFixture(t)
	f.run(t, talk, "talk tom")
	f.run(t, say, "say one")
	f.run(t, talk, "talk tom")

	call, _ := fake.LastCall()
	if len(call.Messages) != 1 {
		t.Errorf("history sent = %+v", call.Messages)
	}
	if len(f.session.Conversation.History) != 2 {
		t.Errorf("history = %+v", f.session.Conversation.History)
	}
}

func TestFailedReplyLeavesTheConversationAsItWas(t *testing.T) {
	f, fake, talk, say, _ := chatFixture(t)
	f.run(t, talk, "talk tom")

	fake.Err = fmt.Errorf("%w: down", llm.ErrRequest)
	if out := f.run(t, say, "say hello"); out != "You say: hello\r\nOld Tom doesn't seem to hear you." {
		t.Errorf("got %q", out)
	}
	if conversation := f.session.Conversation; conversation == nil || len(conversation.History) != 2 {
		t.Errorf("conversation = %+v", conversation)
	}

	fake.Err = nil
	f.run(t, say, "say hello again")
	call, _ := fake.LastCall()
	if len(call.Messages) != 3 || call.Messages[2].Text != "hello again" {
		t.Errorf("history sent = %+v", call.Messages)
	}
}

func TestFailedGreetingStartsNoConversation(t *testing.T) {
	f, fake, talk, _, _ := chatFixture(t)
	fake.Err = fmt.Errorf("%w: down", llm.ErrRequest)
	f.run(t, talk, "talk tom")
	if f.session.Conversation != nil {
		t.Errorf("conversation = %+v", f.session.Conversation)
	}
}

func TestConversationHistoryIsCapped(t *testing.T) {
	f, fake, talk, say, _ := chatFixture(t)
	f.run(t, talk, "talk tom")
	for i := range maxConversationMessages {
		f.run(t, say, fmt.Sprintf("say line %d", i))
	}

	history := f.session.Conversation.History
	if len(history) != maxConversationMessages || history[0].Role != llm.RoleUser {
		t.Fatalf("history has %d messages, starting %+v", len(history), history[0])
	}
	if last := history[len(history)-2]; last.Text != fmt.Sprintf("line %d", maxConversationMessages-1) {
		t.Errorf("last player line = %+v", last)
	}
	call, _ := fake.LastCall()
	if len(call.Messages) > maxConversationMessages+1 { // what's remembered, plus the new line
		t.Errorf("sent %d messages", len(call.Messages))
	}
}

func TestLongSpeechIsCutShortForTheNPC(t *testing.T) {
	f, fake, talk, say, _ := chatFixture(t)
	f.run(t, talk, "talk tom")
	f.run(t, say, "say "+strings.Repeat("a", 2*maxSpeechLength))

	call, _ := fake.LastCall()
	if got := len(call.Messages[len(call.Messages)-1].Text); got != maxSpeechLength {
		t.Errorf("NPC heard %d characters, want %d", got, maxSpeechLength)
	}
}

func TestNPCSystemPrompt(t *testing.T) {
	f, fake, talk, _, tom := chatFixture(t)
	tom.Persona = "Secretly waters down the ale."
	tom.Inventory = []*world.Item{{ID: "rag", Name: "a bar rag"}}
	f.join(t, "bob", worldtest.StartID)
	f.openShop()

	f.run(t, talk, "talk tom")
	call, _ := fake.LastCall()
	start := f.room(worldtest.StartID)
	for _, want := range []string{
		"You are Old Tom", "talking with a player named alice",
		"A grizzled bartender.", "Secretly waters down the ale.",
		"Test Structure", start.Description,
		"Also here: bob", "You're carrying: a bar rag",
		"This is a shop",
		"- " + start.Shop.Stock[0].Name + ": " + world.FormatMoney(start.Shop.Stock[0].Value),
	} {
		if !strings.Contains(call.System, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, call.System)
		}
	}
}

func TestBuildAllSharesTheConversationBetweenTalkAndSay(t *testing.T) {
	f := newFixture(t)
	f.addTom("")
	fake := &llmtest.FakeClient{Reply: "Welcome, traveler."}
	byName := map[string]Command{}
	for _, c := range BuildAll(f.sessions, f.world, f.repo, func() (llm.Client, error) { return fake, nil }, nil) {
		byName[c.Name()] = c
	}

	f.run(t, byName["talk"], "talk tom")
	fake.Reply = "Aye."
	if out := f.run(t, byName["say"], "say hi"); out != "You say: hi\r\nOld Tom says: Aye." {
		t.Errorf("got %q", out)
	}
}
