// Package combat holds the rules of a fight: one attack from one
// combatant against another, and stamina regenerating over time. It
// works the same for players and NPCs -- anything with Stats and an
// inventory is a Combatant -- and does no I/O; the attack command
// narrates the Result.
//
// What each stat does:
//
//   - Dexterity: chance to hit, attacker's against defender's
//     (HitChance).
//   - Strength: damage bonus on top of the wielded weapon's damage
//     (Damage); bare hands is the bonus alone.
//   - Luck: chance a hit is critical, doing double damage (CritChance).
//   - Stamina: every attack costs StaminaCost; with less than that, an
//     attack doesn't happen. It regenerates over time (RunRegen).
//   - Hit points: damage comes off them; dead at or below 0.
//
// All randomness goes through a Rand, so tests can fix the rolls.
package combat

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"mudengine/internal/logging"
	"mudengine/internal/world"
)

var logger = logging.For("mud.combat")

const (
	// StaminaCost is what one attack costs the attacker.
	StaminaCost = 2
	// Hit chance (percent) for evenly matched dexterity, how much each
	// point of difference moves it, and the bounds it's clamped to --
	// nobody is untouchable, and nobody never misses.
	BaseHitChance         = 70
	HitChancePerDexterity = 5
	MinHitChance          = 10
	MaxHitChance          = 95
	// MaxCritChance caps luck's crit chance (percent).
	MaxCritChance = 50
	// RegenInterval is how often everyone regains one stamina.
	RegenInterval = 3 * time.Second
)

// Rand is the source of every roll; IntN returns a value in [0, n).
type Rand interface {
	IntN(n int) int
}

type defaultRand struct{}

func (defaultRand) IntN(n int) int { return rand.IntN(n) }

// Random is the real source of randomness, for use outside tests.
var Random Rand = defaultRand{}

// Combatant is anything that can fight: *world.Player and *world.NPC.
type Combatant interface {
	CombatName() string
	CombatStats() *world.Stats
	// Wielded is the weapon fought with, or nil for bare hands.
	Wielded() *world.Item
}

// Result is what one attack did.
type Result struct {
	// The attacker didn't have the stamina to attack; nothing else
	// happened.
	Tired bool
	// What the attacker swung (nil for bare hands).
	Weapon *world.Item
	Hit    bool
	Crit   bool
	// Hit points taken off the defender.
	Damage int
	// The defender is dead after this attack.
	Killed bool
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// HitChance is the percent chance attacker hits defender.
func HitChance(attacker, defender *world.Stats) int {
	chance := BaseHitChance + HitChancePerDexterity*(attacker.Dexterity-defender.Dexterity)
	return clamp(chance, MinHitChance, MaxHitChance)
}

// CritChance is the percent chance a hit is critical: one per point of
// luck, up to MaxCritChance.
func CritChance(attacker *world.Stats) int {
	return clamp(attacker.Luck, 0, MaxCritChance)
}

// Damage is what a (non-critical) hit does: the weapon's damage plus
// half the attacker's strength, at least 1.
func Damage(attacker *world.Stats, weapon *world.Item) int {
	damage := attacker.Strength / 2
	if weapon != nil && weapon.Weapon != nil {
		damage += weapon.Weapon.Damage
	}
	return max(damage, 1)
}

// Attack resolves one attack: spend the attacker's stamina, then
// Strike with whatever the attacker wields.
func Attack(attacker, defender Combatant, rng Rand) Result {
	a := attacker.CombatStats()
	if a.Stamina < StaminaCost {
		return Result{Tired: true}
	}
	a.Stamina -= StaminaCost
	return Strike(attacker, defender, attacker.Wielded(), rng)
}

// Strike resolves one blow with weapon (nil for bare hands), whatever
// the attacker wields and without touching stamina: roll to hit, roll
// for a crit on a hit, and take the damage off the defender's hit
// points. Rolls happen in that order, each rng.IntN(100). For a blow
// that isn't an ordinary attack -- a thrown spear, whose thrower has
// already paid for the throw.
func Strike(attacker, defender Combatant, weapon *world.Item, rng Rand) Result {
	a, d := attacker.CombatStats(), defender.CombatStats()
	result := Result{Weapon: weapon}
	if rng.IntN(100) >= HitChance(a, d) {
		return result
	}
	result.Hit = true
	result.Damage = Damage(a, result.Weapon)
	if rng.IntN(100) < CritChance(a) {
		result.Crit = true
		result.Damage *= 2
	}
	d.HitPoints -= result.Damage
	result.Killed = d.Dead()
	return result
}

// RegenerateStamina gives one stamina, up to the maximum, to every
// player and NPC with stats currently in the world.
func RegenerateStamina(w *world.World) {
	regen := func(s *world.Stats) {
		if s != nil && s.Stamina < s.MaxStamina {
			s.Stamina++
		}
	}
	for _, room := range w.Rooms() {
		for _, player := range room.Players {
			regen(player.Stats)
		}
		for _, npc := range room.NPCs {
			regen(npc.Stats)
		}
	}
}

// wait blocks for d or until ctx is done -- a variable so tests can run
// the loop without real time passing.
var wait = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RunRegen calls RegenerateStamina every RegenInterval, holding lock
// (the game lock), until ctx is cancelled; then returns ctx's error.
func RunRegen(ctx context.Context, w *world.World, lock sync.Locker) error {
	logger.Debug("Stamina regeneration started", "interval", RegenInterval)
	for {
		if err := wait(ctx, RegenInterval); err != nil {
			return err
		}
		lock.Lock()
		RegenerateStamina(w)
		lock.Unlock()
	}
}
