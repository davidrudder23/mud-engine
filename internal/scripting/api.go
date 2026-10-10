package scripting

// What a script can see and do. Inside a hook:
//
//   - `self`, and any other NPC or item, is an instance table. Reading a
//     field looks at the table itself first (the script's own state),
//     then at the live properties below, then at the class.
//   - Players, rooms, and stats are handles: read-only views with a few
//     methods.
//
// Properties:
//
//	monster: id, name, description, room, stats
//	item:    id, name, description, room, damage (nil unless a weapon)
//	player:  name, room, stats
//	room:    id, description
//	stats:   strength, dexterity, luck, hit_points, max_hit_points,
//	         stamina, max_stamina
//
// Methods:
//
//	monster:say(text)    the room sees "<name> says: <text>"
//	monster:emote(text)  the room sees "<name> <text>"
//	room:send(text)      everyone in the room sees text
//	room:players()       a list of the players in the room
//	room:spawn(id)       spawns an NPC from its definition, returning it
//	                     (nil if the room is full)
//	player:send(text)    that player sees text
//	player:damage(n)     takes n hit points off that player
//
// Nothing here hands a script a Go pointer it could use to change the
// world behind World's back: every change goes through World's own
// methods.

import (
	"strings"

	lua "github.com/yuin/gopher-lua"

	"mudengine/internal/world"
)

func (e *Engine) installAPI() {
	L := e.L

	e.instanceMeta = L.NewTable()
	e.instanceMeta.RawSetString("__index", L.NewFunction(e.instanceIndex))

	e.monsterBase.RawSetString("say", L.NewFunction(e.monsterSay))
	e.monsterBase.RawSetString("emote", L.NewFunction(e.monsterEmote))
	e.monsterBase.RawSetString("on_attacked", L.NewFunction(e.monsterOnAttacked))

	e.playerMeta = e.handleMeta(e.playerProperty, map[string]lua.LGFunction{
		"send":   e.playerSend,
		"damage": e.playerDamage,
	})
	e.roomMeta = e.handleMeta(e.roomProperty, map[string]lua.LGFunction{
		"send":    e.roomSend,
		"players": e.roomPlayers,
		"spawn":   e.roomSpawn,
	})
	e.statsMt = e.handleMeta(statsProperty, nil)
}

// handleMeta builds the metatable for one kind of handle: indexing it
// finds a method first, then a live property. Two handles for the same
// thing compare equal.
func (e *Engine) handleMeta(property func(value any, key string) lua.LValue, methods map[string]lua.LGFunction) *lua.LTable {
	L := e.L
	methodTable := L.NewTable()
	for name, fn := range methods {
		methodTable.RawSetString(name, L.NewFunction(fn))
	}
	meta := L.NewTable()
	meta.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
		handle := L.CheckUserData(1)
		key, ok := L.Get(2).(lua.LString)
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		if method := methodTable.RawGetString(string(key)); method != lua.LNil {
			L.Push(method)
			return 1
		}
		L.Push(property(handle.Value, string(key)))
		return 1
	}))
	meta.RawSetString("__eq", L.NewFunction(func(L *lua.LState) int {
		a, aok := L.Get(1).(*lua.LUserData)
		b, bok := L.Get(2).(*lua.LUserData)
		L.Push(lua.LBool(aok && bok && a.Value == b.Value))
		return 1
	}))
	// Keeps getmetatable from handing a script the real metatable.
	meta.RawSetString("__metatable", lua.LFalse)
	return meta
}

func (e *Engine) newHandle(value any, meta *lua.LTable) lua.LValue {
	handle := e.L.NewUserData()
	handle.Value = value
	e.L.SetMetatable(handle, meta)
	return handle
}

// value converts a Go value into what a script sees: a handle for a
// player, room, or stats; the instance table for an NPC or item; nil
// for a nil pointer.
func (e *Engine) value(v any) lua.LValue {
	switch v := v.(type) {
	case nil:
		return lua.LNil
	case lua.LValue:
		return v
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case *world.Player:
		if v == nil {
			return lua.LNil
		}
		return e.newHandle(v, e.playerMeta)
	case *world.Room:
		if v == nil {
			return lua.LNil
		}
		return e.newHandle(v, e.roomMeta)
	case *world.Stats:
		if v == nil {
			return lua.LNil
		}
		return e.newHandle(v, e.statsMt)
	case *world.NPC:
		if v == nil {
			return lua.LNil
		}
		return e.instance(v, v.Script)
	case *world.Item:
		if v == nil {
			return lua.LNil
		}
		return e.instance(v, v.Script)
	}
	return lua.LNil
}

// room returns the room with this id, or nil if there's none (the id
// of a dead NPC, say).
func (e *Engine) room(id string) *world.Room {
	room, err := e.world.GetRoom(id)
	if err != nil {
		return nil
	}
	return room
}

// itemRoom finds the room item is in: lying there, or carried by
// someone standing there. Nil if it's nowhere.
func (e *Engine) itemRoom(item *world.Item) *world.Room {
	has := func(items []*world.Item) bool {
		for _, candidate := range items {
			if candidate == item {
				return true
			}
		}
		return false
	}
	for _, room := range e.world.Rooms() {
		if has(room.Items) {
			return room
		}
		for _, player := range room.Players {
			if has(player.Inventory) {
				return room
			}
		}
		for _, npc := range room.NPCs {
			if has(npc.Inventory) {
				return room
			}
		}
	}
	return nil
}

// instanceIndex is __index for every instance table: a live property,
// or else whatever the instance's class (or, with no script loaded,
// its base class) has under that name.
func (e *Engine) instanceIndex(L *lua.LState) int {
	b := e.bindings[L.CheckTable(1)]
	key, ok := L.Get(2).(lua.LString)
	if b == nil || !ok {
		L.Push(lua.LNil)
		return 1
	}
	if property := e.instanceProperty(b.obj, string(key)); property != lua.LNil {
		L.Push(property)
		return 1
	}
	L.Push(L.GetField(e.classTable(b), string(key)))
	return 1
}

func (e *Engine) classTable(b *binding) *lua.LTable {
	if loaded := e.active(b.script); loaded != nil {
		return loaded.table
	}
	if _, isItem := b.obj.(*world.Item); isItem {
		return e.itemBase
	}
	return e.monsterBase
}

func (e *Engine) instanceProperty(obj any, key string) lua.LValue {
	switch obj := obj.(type) {
	case *world.NPC:
		switch key {
		case "id":
			return lua.LString(obj.ID)
		case "name":
			return lua.LString(obj.Name)
		case "description":
			return lua.LString(obj.Description)
		case "room":
			return e.value(e.room(obj.RoomID))
		case "stats":
			return e.value(obj.Stats)
		}
	case *world.Item:
		switch key {
		case "id":
			return lua.LString(obj.ID)
		case "name":
			return lua.LString(obj.Name)
		case "description":
			return lua.LString(obj.Description)
		case "room":
			return e.value(e.itemRoom(obj))
		case "damage":
			if obj.Weapon != nil {
				return lua.LNumber(obj.Weapon.Damage)
			}
		}
	}
	return lua.LNil
}

func (e *Engine) playerProperty(value any, key string) lua.LValue {
	player := value.(*world.Player)
	switch key {
	case "name":
		return lua.LString(player.Name)
	case "room":
		return e.value(e.room(player.CurrentRoomID))
	case "stats":
		return e.value(player.Stats)
	}
	return lua.LNil
}

func (e *Engine) roomProperty(value any, key string) lua.LValue {
	room := value.(*world.Room)
	switch key {
	case "id":
		return lua.LString(room.ID)
	case "description":
		return lua.LString(room.Description)
	}
	return lua.LNil
}

func statsProperty(value any, key string) lua.LValue {
	stats := value.(*world.Stats)
	switch key {
	case "strength":
		return lua.LNumber(stats.Strength)
	case "dexterity":
		return lua.LNumber(stats.Dexterity)
	case "luck":
		return lua.LNumber(stats.Luck)
	case "hit_points":
		return lua.LNumber(stats.HitPoints)
	case "max_hit_points":
		return lua.LNumber(stats.MaxHitPoints)
	case "stamina":
		return lua.LNumber(stats.Stamina)
	case "max_stamina":
		return lua.LNumber(stats.MaxStamina)
	}
	return lua.LNil
}

// The check* helpers fetch a method's receiver. Getting it wrong almost
// always means a `.` where a `:` was meant, so the error says so.

func (e *Engine) checkMonster(L *lua.LState) *world.NPC {
	if table, ok := L.Get(1).(*lua.LTable); ok {
		if b := e.bindings[table]; b != nil {
			if npc, ok := b.obj.(*world.NPC); ok {
				return npc
			}
		}
	}
	L.ArgError(1, "expected a monster (call methods with ':', as in self:say(...))")
	return nil
}

func checkHandle[T any](L *lua.LState, what string) T {
	if handle, ok := L.Get(1).(*lua.LUserData); ok {
		if value, ok := handle.Value.(T); ok {
			return value
		}
	}
	L.ArgError(1, "expected a "+what+" (call methods with ':', as in "+what+":send(...))")
	var zero T
	return zero
}

// telnetText turns a script's line endings into the CRLF the telnet
// side expects.
func telnetText(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

// toPlayer shows text to one player. For the player whose command is
// running, that means adding it to the command's reply; for anyone
// else, sending it to their connection.
func (e *Engine) toPlayer(player *world.Player, text string) {
	if player == e.actor {
		e.replies = append(e.replies, text)
		return
	}
	if e.sessions == nil {
		return
	}
	for _, s := range e.sessions.InRoom(player.CurrentRoomID) {
		if s.Player == player {
			s.Send(text)
		}
	}
}

func (e *Engine) toRoom(room *world.Room, text string) {
	for _, player := range room.Players {
		e.toPlayer(player, text)
	}
}

func (e *Engine) monsterSay(L *lua.LState) int {
	npc := e.checkMonster(L)
	text := telnetText(L.CheckString(2))
	if room := e.room(npc.RoomID); room != nil {
		e.toRoom(room, npc.Name+" says: "+text)
	}
	return 0
}

func (e *Engine) monsterEmote(L *lua.LState) int {
	npc := e.checkMonster(L)
	text := telnetText(L.CheckString(2))
	if room := e.room(npc.RoomID); room != nil {
		e.toRoom(room, npc.Name+" "+text)
	}
	return 0
}

// monsterOnAttacked is the base class's on_attacked: the NPC attacks
// back, once. A script that overrides on_attacked without calling this
// has an NPC that doesn't fight back.
func (e *Engine) monsterOnAttacked(L *lua.LState) int {
	e.checkMonster(L)
	if e.strike != nil {
		e.strike()
	}
	return 0
}

func (e *Engine) playerSend(L *lua.LState) int {
	player := checkHandle[*world.Player](L, "player")
	e.toPlayer(player, telnetText(L.CheckString(2)))
	return 0
}

func (e *Engine) playerDamage(L *lua.LState) int {
	player := checkHandle[*world.Player](L, "player")
	e.damage(player, L.CheckInt(2))
	return 0
}

// damage takes amount hit points off player. A player it kills is
// revived the way one killed in a fight is.
func (e *Engine) damage(player *world.Player, amount int) {
	stats := player.Stats
	if amount <= 0 || stats == nil || stats.Dead() {
		return
	}
	stats.HitPoints -= amount
	if stats.Dead() {
		if err := e.world.RevivePlayer(player); err != nil {
			logger.Error("Reviving a player a script killed failed", "player", player.Name, "error", err)
			return
		}
		text := "You collapse, dead.\r\n\r\nYou come to somewhere familiar, fully healed."
		if room := e.room(player.CurrentRoomID); room != nil {
			text += "\r\n" + room.Format()
		}
		e.toPlayer(player, text)
	}
	if e.save != nil && e.ctx != nil {
		if err := e.save(e.ctx, player); err != nil {
			logger.Error("Saving a player a script damaged failed", "player", player.Name, "error", err)
		}
	}
}

func (e *Engine) roomSend(L *lua.LState) int {
	room := checkHandle[*world.Room](L, "room")
	e.toRoom(room, telnetText(L.CheckString(2)))
	return 0
}

func (e *Engine) roomPlayers(L *lua.LState) int {
	room := checkHandle[*world.Room](L, "room")
	players := L.NewTable()
	for _, player := range room.Players {
		players.Append(e.value(player))
	}
	L.Push(players)
	return 1
}

func (e *Engine) roomSpawn(L *lua.LState) int {
	room := checkHandle[*world.Room](L, "room")
	id := L.CheckString(2)
	if _, err := e.world.GetNPC(id); err != nil {
		L.RaiseError("room:spawn: no NPC is defined with id %q", id)
	}
	if len(room.NPCs) >= MaxRoomNPCs {
		L.Push(lua.LNil)
		return 1
	}
	npc, err := e.world.SpawnNPC(id, room.ID)
	if err != nil {
		L.RaiseError("room:spawn: %s", err.Error())
	}
	L.Push(e.value(npc))
	return 1
}
