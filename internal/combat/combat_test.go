package combat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mudengine/internal/combat/combattest"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

func fighter(stats world.Stats, inventory ...*world.Item) *world.Player {
	return &world.Player{Name: "fighter", Stats: &stats, Inventory: inventory}
}

func even() world.Stats {
	return world.Stats{Strength: 6, Dexterity: 5, Luck: 10, HitPoints: 20, MaxHitPoints: 20, Stamina: 10, MaxStamina: 10}
}

func TestHitChance(t *testing.T) {
	// In terms of the tuning constants, so retuning them doesn't break
	// this -- only changing the formula does.
	tests := []struct{ attackerDex, defenderDex, want int }{
		{5, 5, BaseHitChance}, // evenly matched
		{7, 5, BaseHitChance + 2*HitChancePerDexterity},
		{2, 5, BaseHitChance - 3*HitChancePerDexterity},
		{100, 0, MaxHitChance}, // never certain
		{0, 100, MinHitChance}, // never hopeless
	}
	for _, tt := range tests {
		got := HitChance(&world.Stats{Dexterity: tt.attackerDex}, &world.Stats{Dexterity: tt.defenderDex})
		if got != tt.want {
			t.Errorf("dex %d vs %d: %d, want %d", tt.attackerDex, tt.defenderDex, got, tt.want)
		}
	}
}

func TestDamageAndCritChance(t *testing.T) {
	club := &world.Item{Weapon: &world.Weapon{Damage: 6}}
	rag := &world.Item{}
	tests := []struct {
		name     string
		strength int
		weapon   *world.Item
		want     int
	}{
		{"weapon plus half strength", 8, club, 10},
		{"bare hands is half strength", 8, nil, 4},
		{"a non-weapon item is bare hands", 8, rag, 4},
		{"never below 1", 1, nil, 1},
	}
	for _, tt := range tests {
		if got := Damage(&world.Stats{Strength: tt.strength}, tt.weapon); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}

	if CritChance(&world.Stats{Luck: 7}) != 7 || CritChance(&world.Stats{Luck: 80}) != MaxCritChance ||
		CritChance(&world.Stats{Luck: -2}) != 0 {
		t.Error("crit chance isn't luck, clamped to [0, MaxCritChance]")
	}
}

func TestAttack(t *testing.T) {
	club := &world.Item{ID: "club", Weapon: &world.Weapon{Damage: 6}}
	stats := even()
	hit := HitChance(&stats, &stats) // evenly matched
	crit := CritChance(&stats)
	damage := Damage(&stats, club)
	tests := []struct {
		name  string
		rolls []int // to-hit, then crit
		want  Result
	}{
		{"hit, at the edge of the hit chance", []int{hit - 1, crit}, Result{Weapon: club, Hit: true, Damage: damage}},
		{"crit doubles damage", []int{0, crit - 1}, Result{Weapon: club, Hit: true, Crit: true, Damage: 2 * damage}},
		{"miss, at the edge of the hit chance", []int{hit}, Result{Weapon: club}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attacker, defender := fighter(even(), club), fighter(even())

			got := Attack(attacker, defender, combattest.NewRolls(tt.rolls...))

			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if wantHP := stats.HitPoints - tt.want.Damage; defender.Stats.HitPoints != wantHP {
				t.Errorf("defender hp = %d, want %d", defender.Stats.HitPoints, wantHP)
			}
			if attacker.Stats.Stamina != stats.Stamina-StaminaCost {
				t.Errorf("stamina = %d, want %d", attacker.Stats.Stamina, stats.Stamina-StaminaCost)
			}
		})
	}
}

func TestAttackKills(t *testing.T) {
	club := &world.Item{Weapon: &world.Weapon{Damage: 6}}
	attacker, defender := fighter(even(), club), fighter(even())
	defender.Stats.HitPoints = Damage(attacker.Stats, club) + 1

	// One hit short of dead: hit points above 0 are alive...
	if got := Attack(attacker, defender, combattest.NewRolls(0)); got.Killed || defender.Stats.Dead() {
		t.Fatalf("killed at 1 hp left: %+v", got)
	}
	// ...and exactly 0 is dead.
	defender.Stats.HitPoints = Damage(attacker.Stats, club)
	if got := Attack(attacker, defender, combattest.NewRolls(0)); !got.Killed || defender.Stats.HitPoints != 0 {
		t.Errorf("got %+v, hp %d", got, defender.Stats.HitPoints)
	}
}

func TestAttackWhileTired(t *testing.T) {
	attacker, defender := fighter(even()), fighter(even())
	attacker.Stats.Stamina = StaminaCost - 1

	got := Attack(attacker, defender, combattest.NewRolls(0)) // would hit, if it swung
	if got != (Result{Tired: true}) || attacker.Stats.Stamina != StaminaCost-1 || defender.Stats.HitPoints != even().HitPoints {
		t.Errorf("got %+v", got)
	}
}

func TestAttackWorksBetweenPlayersAndNPCs(t *testing.T) {
	stats := even()
	troll := &world.NPC{Name: "A Troll", Enemy: true, Stats: &stats}
	player := fighter(even())

	want := even().HitPoints - Damage(&stats, nil) // bare hands, no crit
	if got := Attack(troll, player, combattest.NewRolls(0)); !got.Hit || player.Stats.HitPoints != want {
		t.Errorf("npc -> player: %+v, hp %d", got, player.Stats.HitPoints)
	}
	if got := Attack(player, troll, combattest.NewRolls(0)); !got.Hit || troll.Stats.HitPoints != want {
		t.Errorf("player -> npc: %+v, hp %d", got, troll.Stats.HitPoints)
	}
}

func TestRegenerateStamina(t *testing.T) {
	w := worldtest.TwoRooms()
	start := worldtest.MustRoom(w, worldtest.StartID)
	tired := fighter(even())
	tired.Stats.Stamina = 3
	rested := fighter(even())
	helper := &world.NPC{Name: "Grubb"} // no stats: skipped
	enemyStats := even()
	enemyStats.Stamina = 0
	troll := &world.NPC{Name: "A Troll", Enemy: true, Stats: &enemyStats}
	start.Players = append(start.Players, tired, rested)
	start.NPCs = append(start.NPCs, helper, troll)

	RegenerateStamina(w)

	if tired.Stats.Stamina != 4 || rested.Stats.Stamina != 10 || troll.Stats.Stamina != 1 {
		t.Errorf("stamina: tired=%d rested=%d troll=%d", tired.Stats.Stamina, rested.Stats.Stamina, troll.Stats.Stamina)
	}
}

func TestRunRegenTicksUntilCancelled(t *testing.T) {
	w := worldtest.TwoRooms()
	player := fighter(even())
	player.Stats.Stamina = 0
	start := worldtest.MustRoom(w, worldtest.StartID)
	start.Players = append(start.Players, player)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := 0
	original := wait
	t.Cleanup(func() { wait = original })
	wait = func(ctx context.Context, _ time.Duration) error {
		ticks++
		if ticks > 4 {
			cancel()
			return ctx.Err()
		}
		return nil
	}

	if err := RunRegen(ctx, w, &sync.Mutex{}); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
	if player.Stats.Stamina != 4 {
		t.Errorf("stamina = %d after 4 ticks", player.Stats.Stamina)
	}
}

// Strike is a blow with a chosen weapon: not necessarily the best one
// carried, and with no stamina spent or needed.
func TestStrikeUsesTheGivenWeaponAndNoStamina(t *testing.T) {
	spear := &world.Item{Weapon: &world.Weapon{Damage: 2, Type: world.WeaponSpear}}
	club := &world.Item{Weapon: &world.Weapon{Damage: 9}}
	attacker := fighter(even(), club)
	attacker.Stats.Stamina = 0
	defender := fighter(even())

	result := Strike(attacker, defender, spear, combattest.NewRolls(0, 99))

	if !result.Hit || result.Weapon != spear || result.Damage != Damage(attacker.Stats, spear) {
		t.Errorf("result = %+v", result)
	}
	if defender.Stats.HitPoints != defender.Stats.MaxHitPoints-result.Damage {
		t.Errorf("defender hit points = %d", defender.Stats.HitPoints)
	}
	if attacker.Stats.Stamina != 0 {
		t.Errorf("attacker stamina = %d", attacker.Stats.Stamina)
	}
}
