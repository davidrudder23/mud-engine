package commands

import (
	"context"
	"testing"

	"mudengine/internal/session"
	"mudengine/internal/world/worldtest"
)

// join logs another player in at roomID, alongside the fixture's alice.
func (f *fixture) join(t *testing.T, name, roomID string) *session.Session {
	t.Helper()
	player, err := f.repo.Create(context.Background(), name, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.world.PlacePlayer(player); err != nil {
		t.Fatal(err)
	}
	return f.sessions.Create(player)
}

// received drains everything sent to s so far.
func received(s *session.Session) []string {
	var messages []string
	for {
		select {
		case message := <-s.Messages():
			messages = append(messages, message)
		default:
			return messages
		}
	}
}

func TestSayReachesEveryoneElseInTheRoom(t *testing.T) {
	f := newFixture(t)
	bob := f.join(t, "bob", worldtest.StartID)
	carol := f.join(t, "carol", worldtest.StartID)
	dave := f.join(t, "dave", worldtest.NorthID)
	say := NewSayCommand(f.sessions)

	if out := f.run(t, say, "say Hello, there!"); out != "You say: Hello, there!" {
		t.Errorf("speaker got %q", out)
	}
	for _, listener := range []*session.Session{bob, carol} {
		got := received(listener)
		if len(got) != 1 || got[0] != "alice says: Hello, there!" {
			t.Errorf("%s heard %q", listener.Player.Name, got)
		}
	}
	if got := received(dave); len(got) != 0 {
		t.Errorf("dave, in another room, heard %q", got)
	}
	if got := received(f.session); len(got) != 0 {
		t.Errorf("speaker was sent her own message: %q", got)
	}
}

func TestSayWithNothingToSay(t *testing.T) {
	f := newFixture(t)
	bob := f.join(t, "bob", worldtest.StartID)
	say := NewSayCommand(f.sessions)

	for _, line := range []string{"say", "say   "} {
		if out := f.run(t, say, line); out != "Say what?" {
			t.Errorf("%q: %q", line, out)
		}
	}
	if got := received(bob); len(got) != 0 {
		t.Errorf("bob heard %q", got)
	}
}

func TestSayReachesTheSameNamesOtherSession(t *testing.T) {
	f := newFixture(t)
	other := f.join(t, "alice2", worldtest.StartID)
	other.Player.Name = "alice"
	say := NewSayCommand(f.sessions)

	f.run(t, say, "say hi")
	if got := received(other); len(got) != 1 || got[0] != "alice says: hi" {
		t.Errorf("other session heard %q", got)
	}
}

func TestSayDropsMessagesForAListenerWhoStoppedReading(t *testing.T) {
	f := newFixture(t)
	bob := f.join(t, "bob", worldtest.StartID)
	say := NewSayCommand(f.sessions)

	// More than bob's outbox holds; none of it may block the speaker.
	for range 200 {
		f.run(t, say, "say spam")
	}
	if got := received(bob); len(got) == 0 || len(got) >= 200 {
		t.Errorf("bob has %d messages queued", len(got))
	}
}
