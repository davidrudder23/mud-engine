package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/repository"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// The three commands for a room with a world.Shop: browse, buy, and
// sell. Money is Ankh-Morpork dollars, on the player (Player.Money) and
// saved after every sale either way.
//
// A shop charges an item's Value and pays world.SellPrice for one --
// half that. It sells only its own stock, which never runs out, but
// buys anything worth something; what it buys isn't put on sale, it's
// just gone.

// inShop provides MatchRoom for the commands only a shop makes usable,
// so `help` lists them only there.
type inShop struct{}

func (inShop) MatchRoom(room *world.Room) bool { return room.Shop != nil }

// noShop is every shop command's reply anywhere else.
const noShop = "There's no shop here."

// describeWare is an item's name plus, for a weapon, what it does: "a short
// sword (4 damage)", "a hunting spear (5 damage, can be thrown)".
func describeWare(item *world.Item) string {
	if item.Weapon == nil {
		return item.Format()
	}
	if item.Throwable() {
		return fmt.Sprintf("%s (%d damage, can be thrown)", item.Format(), item.Weapon.Damage)
	}
	return fmt.Sprintf("%s (%d damage)", item.Format(), item.Weapon.Damage)
}

// purse is the line telling a player what they have to spend.
func purse(player *world.Player) string {
	return "You have " + world.FormatMoney(player.Money) + "."
}

// BrowseCommand lists what the shop in the current room sells, with
// prices, and what the player has to spend. Ignores anything typed
// after the verb. Also answers to "wares".
type BrowseCommand struct {
	inShop
	verbs
	sessions *session.Registry
	world    *world.World
}

func NewBrowseCommand(sessions *session.Registry, w *world.World) *BrowseCommand {
	return &BrowseCommand{verbs: verbs{"browse", "wares"}, sessions: sessions, world: w}
}

func (c *BrowseCommand) Describe() string        { return "browse      lists what the shop here sells" }
func (c *BrowseCommand) Parameters() []Parameter { return nil }

func (c *BrowseCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	room, err := c.world.GetRoom(s.Player.CurrentRoomID)
	if err != nil {
		return "", err
	}
	if room.Shop == nil {
		return noShop, nil
	}

	lines := []string{"For sale:"}
	for _, item := range room.Shop.Stock {
		lines = append(lines, "  - "+describeWare(item)+" -- "+world.FormatMoney(item.Value))
	}
	if len(room.Shop.Stock) == 0 {
		lines = []string{"There's nothing for sale."}
	}
	return strings.Join(append(lines, purse(s.Player)), "\r\n"), nil
}

// BuyCommand buys one of the shop's wares: the player pays its Value
// and gets their own copy. Matches the stock the way `pick up` matches
// a room's items. Also answers to "purchase".
type BuyCommand struct {
	inShop
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
}

func NewBuyCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *BuyCommand {
	return &BuyCommand{verbs: verbs{"buy", "purchase"}, sessions: sessions, world: w, players: players}
}

func (c *BuyCommand) Describe() string { return "buy <item>       buys something from the shop here" }
func (c *BuyCommand) Parameters() []Parameter {
	return []Parameter{{Name: "item", Type: String, Required: true}}
}

func (c *BuyCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player
	room, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}
	if room.Shop == nil {
		return noShop, nil
	}

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Buy what?", nil
	}
	ware := findItem(room.Shop.Stock, query)
	if ware == nil {
		return "The shop doesn't sell " + quoted(query) + ".", nil
	}
	if player.Money < ware.Value {
		return fmt.Sprintf("%s costs %s, and you only have %s.",
			capitalized(ware.Format()), world.FormatMoney(ware.Value), world.FormatMoney(player.Money)), nil
	}

	player.Money -= ware.Value
	player.Inventory = append(player.Inventory, room.Shop.Take(ware))
	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	return fmt.Sprintf("You buy %s for %s. %s", ware.Format(), world.FormatMoney(ware.Value), purse(player)), nil
}

// SellCommand sells something the player is carrying to the shop, for
// world.SellPrice. Matches the inventory the way `drop` does. The shop
// turns down anything with no Value.
type SellCommand struct {
	inShop
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
}

func NewSellCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *SellCommand {
	return &SellCommand{verbs: verbs{"sell"}, sessions: sessions, world: w, players: players}
}

func (c *SellCommand) Describe() string {
	return "sell <item>      sells something you're carrying to the shop here"
}
func (c *SellCommand) Parameters() []Parameter {
	return []Parameter{{Name: "item", Type: String, Required: true}}
}

func (c *SellCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player
	room, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}
	if room.Shop == nil {
		return noShop, nil
	}

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Sell what?", nil
	}
	item := findItem(player.Inventory, query)
	if item == nil {
		return "You aren't carrying " + quoted(query) + ".", nil
	}
	price := world.SellPrice(item)
	if price == 0 {
		return "The shop has no use for " + item.Format() + ".", nil
	}

	player.Inventory, _ = world.RemoveItem(player.Inventory, item)
	player.Money += price
	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	return fmt.Sprintf("You sell %s for %s. %s", item.Format(), world.FormatMoney(price), purse(player)), nil
}
