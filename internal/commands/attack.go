package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/combat"
	"mudengine/internal/repository"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// AttackCommand fights an enemy NPC in the room, one exchange per
// command: the player attacks, and if the enemy survives it strikes
// back. The rules are the combat package's; this narrates them.
//
// An enemy at or below 0 hit points dies: everything it carried drops
// into the room (for `pick up`) and it's gone. A player at or below 0
// hit points comes to in the starting room, fully healed, still
// carrying everything. Only enemies can be attacked; anyone else
// "isn't looking for a fight". The player's stats are saved after
// every exchange.
type AttackCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
	// Every roll in a fight; tests replace it with fixed rolls.
	rng combat.Rand
}

func NewAttackCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *AttackCommand {
	return &AttackCommand{verbs: verbs{"attack"}, sessions: sessions, world: w, players: players, rng: combat.Random}
}

func (c *AttackCommand) Describe() string { return "attack <npc>    fights an enemy in the room" }
func (c *AttackCommand) Parameters() []Parameter {
	return []Parameter{{Name: "npc", Type: String, Required: true}}
}

func (c *AttackCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player
	if player.Stats == nil {
		return "", fmt.Errorf("player %q has no stats", player.Name)
	}
	room, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Attack whom?", nil
	}
	npc := findNPC(room.NPCs, query)
	if npc == nil {
		return "You don't see " + quoted(query) + " here.", nil
	}
	if !npc.Enemy || npc.Stats == nil {
		return npc.Name + " isn't looking for a fight.", nil
	}

	lines, err := c.exchange(player, npc)
	if err != nil {
		return "", err
	}
	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	return strings.Join(lines, "\r\n"), nil
}

// exchange is one round: the player's attack, then the enemy's, with
// whichever deaths follow.
func (c *AttackCommand) exchange(player *world.Player, npc *world.NPC) ([]string, error) {
	hit := combat.Attack(player, npc, c.rng)
	lines := []string{describePlayerAttack(npc, hit)}

	if hit.Killed {
		dropped, err := c.world.KillNPC(npc)
		if err != nil {
			return nil, err
		}
		lines = append(lines, npc.Name+" collapses, dead.")
		if len(dropped) > 0 {
			lines = append(lines, "It drops "+itemList(dropped)+".")
		}
		return lines, nil
	}

	back := combat.Attack(npc, player, c.rng)
	lines = append(lines, describeEnemyAttack(npc, back))

	if back.Killed {
		player.Stats.Restore()
		if err := c.world.MovePlayer(player, c.world.StartingRoomID()); err != nil {
			return nil, err
		}
		room, err := c.world.GetRoom(player.CurrentRoomID)
		if err != nil {
			return nil, err
		}
		return append(lines,
			"You collapse, dead.",
			"",
			"You come to somewhere familiar, fully healed.",
			room.Format(),
		), nil
	}

	stats := player.Stats
	return append(lines, fmt.Sprintf("%s looks %s. You have %d/%d hit points and %d/%d stamina.",
		npc.Name, npc.Stats.Condition(), stats.HitPoints, stats.MaxHitPoints, stats.Stamina, stats.MaxStamina)), nil
}

func describePlayerAttack(npc *world.NPC, r combat.Result) string {
	target := asObject(npc.Name)
	switch {
	case r.Tired:
		return "You're too tired to attack."
	case !r.Hit:
		return "You swing at " + target + " and miss."
	case r.Crit:
		return fmt.Sprintf("You land a critical hit on %s%s for %d damage!", target, with(r.Weapon), r.Damage)
	default:
		return fmt.Sprintf("You hit %s%s for %d damage.", target, with(r.Weapon), r.Damage)
	}
}

func describeEnemyAttack(npc *world.NPC, r combat.Result) string {
	switch {
	case r.Tired:
		return npc.Name + " is too winded to strike back."
	case !r.Hit:
		return npc.Name + " swings at you and misses."
	case r.Crit:
		return fmt.Sprintf("%s lands a critical hit on you%s for %d damage!", npc.Name, with(r.Weapon), r.Damage)
	default:
		return fmt.Sprintf("%s hits you%s for %d damage.", npc.Name, with(r.Weapon), r.Damage)
	}
}

// with is " with <weapon>", or "" for bare hands.
func with(weapon *world.Item) string {
	if weapon == nil {
		return ""
	}
	return " with " + weapon.Format()
}

// asObject lowercases a leading article so a name reads naturally
// mid-sentence: "A Troll" -> "a Troll"; "Grubb" is unchanged.
func asObject(name string) string {
	for _, article := range []string{"A ", "An ", "The "} {
		if strings.HasPrefix(name, article) {
			return strings.ToLower(article) + name[len(article):]
		}
	}
	return name
}

// itemList joins item names: "a club", "a club and a key", "a club, a
// key, and a rag".
func itemList(items []*world.Item) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Format())
	}
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

// StatsCommand shows the player's own stats and what they're fighting
// with.
type StatsCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
}

func NewStatsCommand(sessions *session.Registry) *StatsCommand {
	return &StatsCommand{verbs: verbs{"stats"}, sessions: sessions}
}

func (c *StatsCommand) Describe() string {
	return "stats       shows your hit points, stamina, and stats"
}
func (c *StatsCommand) Parameters() []Parameter { return nil }

func (c *StatsCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	stats := s.Player.Stats
	if stats == nil {
		return "", fmt.Errorf("player %q has no stats", s.Player.Name)
	}
	wielding := "nothing (bare hands)"
	if weapon := s.Player.Wielded(); weapon != nil {
		wielding = fmt.Sprintf("%s (%d damage)", weapon.Format(), weapon.Weapon.Damage)
	}
	return strings.Join([]string{
		fmt.Sprintf("Hit points: %d/%d", stats.HitPoints, stats.MaxHitPoints),
		fmt.Sprintf("Stamina:    %d/%d", stats.Stamina, stats.MaxStamina),
		fmt.Sprintf("Strength %d, Dexterity %d, Luck %d", stats.Strength, stats.Dexterity, stats.Luck),
		"Wielding:   " + wielding,
	}, "\r\n"), nil
}
