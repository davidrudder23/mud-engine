package commands

import (
	"context"
	"slices"
	"strings"
	"testing"

	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

// openShop turns the starting room into a shop selling a dagger (AM$3),
// a short sword (AM$10), and a spear (AM$14).
func (f *fixture) openShop() *world.Shop {
	shop := &world.Shop{Stock: []*world.Item{
		{ID: "dagger", Name: "a dagger", Value: 3, Weapon: &world.Weapon{Damage: 2}},
		{ID: "short-sword", Name: "a short sword", Value: 10, Weapon: &world.Weapon{Damage: 4}},
		{ID: "spear", Name: "a hunting spear", Value: 14, Weapon: &world.Weapon{Damage: 5, Type: world.WeaponSpear}},
		{ID: "whetstone", Name: "a whetstone", Value: 1},
	}}
	f.room(worldtest.StartID).Shop = shop
	return shop
}

// saved is alice's money as the repository has it.
func (f *fixture) saved(t *testing.T) int {
	t.Helper()
	player, err := f.repo.GetByID(context.Background(), f.player.ID)
	if err != nil {
		t.Fatal(err)
	}
	return player.Money
}

func TestNewPlayerStartsWithMoney(t *testing.T) {
	f := newFixture(t)
	if f.player.Money != world.DefaultPlayerMoney {
		t.Errorf("money = %d", f.player.Money)
	}
}

func TestShopCommandsNeedAShop(t *testing.T) {
	f := newFixture(t)
	f.player.Inventory = []*world.Item{{ID: "rock", Name: "a rock", Value: 4}}
	for line, command := range map[string]Command{
		"browse":    NewBrowseCommand(f.sessions, f.world),
		"buy rock":  NewBuyCommand(f.sessions, f.world, f.repo),
		"sell rock": NewSellCommand(f.sessions, f.world, f.repo),
	} {
		if out := f.run(t, command, line); out != "There's no shop here." {
			t.Errorf("%s: %q", line, out)
		}
		if command.MatchRoom(f.room(worldtest.StartID)) {
			t.Errorf("%s: matches a room with no shop", line)
		}
	}
	if len(f.player.Inventory) != 1 || f.player.Money != world.DefaultPlayerMoney {
		t.Errorf("inventory=%v money=%d", f.player.Inventory, f.player.Money)
	}

	f.openShop()
	if !NewBuyCommand(f.sessions, f.world, f.repo).MatchRoom(f.room(worldtest.StartID)) {
		t.Error("buy doesn't match a room with a shop")
	}
}

func TestBrowse(t *testing.T) {
	f := newFixture(t)
	f.openShop()
	browse := NewBrowseCommand(f.sessions, f.world)

	want := strings.Join([]string{
		"For sale:",
		"  - a dagger (2 damage) -- AM$3",
		"  - a short sword (4 damage) -- AM$10",
		"  - a hunting spear (5 damage, can be thrown) -- AM$14",
		"  - a whetstone -- AM$1",
		"You have AM$10.",
	}, "\r\n")
	if out := f.run(t, browse, "browse"); out != want {
		t.Errorf("browse = %q", out)
	}
	if out := f.run(t, browse, "wares please"); out != want {
		t.Errorf("wares = %q", out)
	}

	f.room(worldtest.StartID).Shop = &world.Shop{}
	if out := f.run(t, browse, "browse"); out != "There's nothing for sale.\r\nYou have AM$10." {
		t.Errorf("empty shop = %q", out)
	}
}

func TestBuy(t *testing.T) {
	f := newFixture(t)
	shop := f.openShop()
	buy := NewBuyCommand(f.sessions, f.world, f.repo)

	if out := f.run(t, buy, "buy"); out != "Buy what?" {
		t.Errorf("no item: %q", out)
	}
	if out := f.run(t, buy, "buy halberd"); out != "The shop doesn't sell 'halberd'." {
		t.Errorf("not stocked: %q", out)
	}
	if out := f.run(t, buy, "buy spear"); out != "A hunting spear costs AM$14, and you only have AM$10." {
		t.Errorf("too dear: %q", out)
	}
	if len(f.player.Inventory) != 0 || f.player.Money != 10 {
		t.Fatalf("inventory=%v money=%d", f.player.Inventory, f.player.Money)
	}

	if out := f.run(t, buy, "buy Dagger"); out != "You buy a dagger for AM$3. You have AM$7." {
		t.Errorf("buy: %q", out)
	}
	if out := f.run(t, buy, "purchase dagger"); out != "You buy a dagger for AM$3. You have AM$4." {
		t.Errorf("purchase: %q", out)
	}
	// Two daggers of the player's own, and the shop still has its one.
	inventory := f.player.Inventory
	if len(inventory) != 2 || inventory[0] == inventory[1] || slices.Contains(inventory, shop.Stock[0]) {
		t.Errorf("inventory = %v", inventory)
	}
	if len(shop.Stock) != 4 || f.player.Wielded() != inventory[0] {
		t.Errorf("stock=%d wielded=%v", len(shop.Stock), f.player.Wielded())
	}
	if f.player.Money != 4 || f.saved(t) != 4 {
		t.Errorf("money=%d saved=%d", f.player.Money, f.saved(t))
	}
}

func TestBuyWithExactlyEnough(t *testing.T) {
	f := newFixture(t)
	f.openShop()
	buy := NewBuyCommand(f.sessions, f.world, f.repo)
	if out := f.run(t, buy, "buy short sword"); out != "You buy a short sword for AM$10. You have AM$0." {
		t.Errorf("buy: %q", out)
	}
}

func TestSell(t *testing.T) {
	f := newFixture(t)
	shop := f.openShop()
	sell := NewSellCommand(f.sessions, f.world, f.repo)
	teapot := &world.Item{ID: "silver-teapot", Name: "a dented silver teapot", Value: 12}
	tankard := &world.Item{ID: "tankard", Name: "a half-finished tankard"}
	f.player.Inventory = []*world.Item{teapot, tankard}

	if out := f.run(t, sell, "sell"); out != "Sell what?" {
		t.Errorf("no item: %q", out)
	}
	if out := f.run(t, sell, "sell sword"); out != "You aren't carrying 'sword'." {
		t.Errorf("not carried: %q", out)
	}
	if out := f.run(t, sell, "sell tankard"); out != "The shop has no use for a half-finished tankard." {
		t.Errorf("worthless: %q", out)
	}
	if len(f.player.Inventory) != 2 || f.player.Money != 10 {
		t.Fatalf("inventory=%v money=%d", f.player.Inventory, f.player.Money)
	}

	if out := f.run(t, sell, "sell teapot"); out != "You sell a dented silver teapot for AM$6. You have AM$16." {
		t.Errorf("sell: %q", out)
	}
	if !slices.Equal(f.player.Inventory, []*world.Item{tankard}) || f.saved(t) != 16 {
		t.Errorf("inventory=%v saved=%d", f.player.Inventory, f.saved(t))
	}
	// What the shop buys doesn't go on sale, or onto the floor.
	if len(shop.Stock) != 4 || len(f.room(worldtest.StartID).Items) != 0 {
		t.Errorf("stock=%d room=%v", len(shop.Stock), f.room(worldtest.StartID).Items)
	}
}

func TestSellingBackWhatYouBoughtLosesHalf(t *testing.T) {
	f := newFixture(t)
	f.openShop()
	f.run(t, NewBuyCommand(f.sessions, f.world, f.repo), "buy short sword")
	out := f.run(t, NewSellCommand(f.sessions, f.world, f.repo), "sell short sword")
	if out != "You sell a short sword for AM$5. You have AM$5." || len(f.player.Inventory) != 0 {
		t.Errorf("sell: %q inventory=%v", out, f.player.Inventory)
	}
}

func TestRunnerHelpListsShopCommandsOnlyInAShop(t *testing.T) {
	f, runner, _ := runnerFixture(t)
	listed := func() bool {
		return strings.Contains(f.runLine(t, runner, "help"), "buy <item>")
	}
	if listed() {
		t.Error("help lists buy where there's no shop")
	}
	f.openShop()
	if !listed() {
		t.Error("help doesn't list buy in a shop")
	}
	if out := f.runLine(t, runner, "buy dagger"); out != "You buy a dagger for AM$3. You have AM$7." {
		t.Errorf("buy through the runner: %q", out)
	}
}
