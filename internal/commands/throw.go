package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/combat"
	"mudengine/internal/repository"
	"mudengine/internal/scripting"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// ThrowCommand throws a spear in a direction: "throw spear east". The
// last word is the direction; everything before it names the item,
// matched against the player's inventory the way `drop` does. Only a
// spear (an item whose weapon type is world.WeaponSpear) can be thrown.
//
// The spear looks for an enemy in the thrower's own room first. If
// there's none it flies through the doorway in that direction and
// looks again, and so on, room after room, until it finds an enemy or
// there's no doorway that way. (Or it would come back to a room it has
// already been through: doorways can lead in circles, a flight can't.)
//
// At the first enemy it finds, it's one blow by the thrower with the
// spear, under the usual combat rules (combat.Strike) -- it can miss.
// Either way the flight ends there. The enemy doesn't get to strike
// back, though a scripted one still gets its on_attacked; one the
// spear kills dies as it would in a fight.
//
// A throw costs the same stamina as an attack, whatever it finds. The
// spear ends up on the floor of the room where its flight ended, and
// the thrower is told how far that was. Other players along the way
// see it go.
type ThrowCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
	// Every roll; tests replace it with fixed rolls.
	rng combat.Rand
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
}

func NewThrowCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *ThrowCommand {
	return &ThrowCommand{verbs: verbs{"throw"}, sessions: sessions, world: w, players: players, rng: combat.Random}
}

func (c *ThrowCommand) Describe() string {
	return `throw <item> <dir>  throws a spear through the rooms that way (e.g. "throw spear east")`
}
func (c *ThrowCommand) Parameters() []Parameter {
	return []Parameter{
		{Name: "item", Type: String, Required: true},
		{Name: "direction", Type: String, Required: true},
	}
}

// findEnemy returns the first NPC in room that can be fought, or nil.
func findEnemy(room *world.Room) *world.NPC {
	for _, npc := range room.NPCs {
		if npc.Enemy && npc.Stats != nil {
			return npc
		}
	}
	return nil
}

// findDoorway returns room's doorway in direction (case-insensitive),
// or nil.
func findDoorway(room *world.Room, direction string) *world.Doorway {
	for _, doorway := range room.Doorways {
		if strings.EqualFold(doorway.Direction, direction) {
			return doorway
		}
	}
	return nil
}

// capitalized uppercases s's first letter, to start a sentence with an
// item's name ("a spear" -> "A spear").
func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// distance says how far away something happened, to start a sentence
// with: "" in this room, then "In the next room, ", "2 rooms away, ".
func distance(rooms int) string {
	switch rooms {
	case 0:
		return ""
	case 1:
		return "In the next room, "
	default:
		return fmt.Sprintf("%d rooms away, ", rooms)
	}
}

// tell sends text to every session in roomID but the thrower's own.
func (c *ThrowCommand) tell(roomID string, thrower uuid.UUID, text string) {
	for _, s := range c.sessions.InRoom(roomID) {
		if s.ID != thrower {
			s.Send(text)
		}
	}
}

func (c *ThrowCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player
	if player.Stats == nil {
		return "", fmt.Errorf("player %q has no stats", player.Name)
	}
	origin, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}

	words := strings.Fields(strings.ToLower(c.argument(line)))
	switch len(words) {
	case 0:
		return "Throw what?", nil
	case 1:
		return "Throw " + words[0] + " which way?", nil
	}
	direction := words[len(words)-1]
	query := strings.Join(words[:len(words)-1], " ")

	spear := findItem(player.Inventory, query)
	if spear == nil {
		return "You aren't carrying " + quoted(query) + ".", nil
	}
	if !spear.Throwable() {
		return "You can't throw " + spear.Format() + ".", nil
	}
	if player.Stats.Stamina < combat.StaminaCost {
		return "You're too tired to throw anything.", nil
	}

	player.Stats.Stamina -= combat.StaminaCost
	player.Inventory, _ = world.RemoveItem(player.Inventory, spear)
	lines := []string{"You throw " + spear.Format() + " " + direction + "."}
	c.tell(origin.ID, sessionID, player.Name+" throws "+spear.Format()+" "+direction+".")

	// The flight: room by room until there's an enemy or no way on.
	room, flown := origin, 0
	visited := map[string]bool{origin.ID: true}
	var target *world.NPC
	for {
		if target = findEnemy(room); target != nil {
			break
		}
		doorway := findDoorway(room, direction)
		if doorway == nil || visited[doorway.To] {
			break
		}
		if room != origin {
			c.tell(room.ID, sessionID, capitalized(spear.Format())+" flies through, heading "+direction+".")
		}
		if room, err = c.world.GetRoom(doorway.To); err != nil {
			return "", err
		}
		visited[room.ID] = true
		flown++
	}
	room.Items = append(room.Items, spear)

	arrives := capitalized(spear.Format())
	if flown > 0 {
		arrives += " flies in and"
	}
	switch {
	case target == nil && flown == 0:
		lines = append(lines, "There's nothing to hit and no way "+direction+": it clatters to the floor.")
		c.tell(room.ID, sessionID, arrives+" clatters to the floor.")
	case target == nil:
		rooms := "rooms"
		if flown == 1 {
			rooms = "room"
		}
		lines = append(lines, fmt.Sprintf("It flies %d %s %s without finding anything to hit, and clatters to the floor.", flown, rooms, direction))
		c.tell(room.ID, sessionID, arrives+" clatters to the floor.")
	default:
		more, err := c.strike(ctx, player, sessionID, spear, target, room, flown, arrives)
		if err != nil {
			return "", err
		}
		lines = append(lines, more...)
	}

	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	return strings.Join(lines, "\r\n"), nil
}

// strike is the spear reaching target, flown rooms from the thrower:
// one blow, and the target's death if it comes to that.
func (c *ThrowCommand) strike(ctx context.Context, player *world.Player, sessionID uuid.UUID, spear *world.Item, target *world.NPC, room *world.Room, flown int, arrives string) ([]string, error) {
	where, name := distance(flown), asObject(target.Name)
	hit := combat.Strike(player, target, spear, c.rng)
	if !hit.Hit {
		c.tell(room.ID, sessionID, arrives+" narrowly misses "+name+".")
		return []string{capitalized(where + "it flies past " + name + " and clatters to the floor.")}, nil
	}

	var lines []string
	if hit.Crit {
		lines = append(lines, capitalized(fmt.Sprintf("%sit lands a critical hit on %s for %d damage!", where, name, hit.Damage)))
	} else {
		lines = append(lines, capitalized(fmt.Sprintf("%sit hits %s for %d damage.", where, name, hit.Damage)))
	}
	c.tell(room.ID, sessionID, arrives+" hits "+name+".")
	lines = append(lines, c.scripts.ItemHit(ctx, spear, player, target, player)...)

	if !hit.Killed {
		// No striking back at someone who may not even be in the room,
		// but a scripted enemy still knows it's been hit.
		return append(lines, c.scripts.NPCAttacked(ctx, target, player, func() {})...), nil
	}

	lines = append(lines, target.Name+" collapses, dead.")
	c.tell(room.ID, sessionID, target.Name+" collapses, dead.")
	// While it's still in the room, as in a fight.
	lines = append(lines, c.scripts.NPCDied(ctx, target, player)...)
	dropped, err := c.world.KillNPC(target)
	if err != nil {
		return nil, err
	}
	if len(dropped) > 0 {
		lines = append(lines, "It drops "+itemList(dropped)+".")
	}
	return lines, nil
}
