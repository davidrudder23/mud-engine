package world

import "fmt"

// Shop is what makes a Room somewhere to buy and sell things (see the
// browse, buy, and sell commands). It's part of the room, not of
// whoever stands behind the counter: a shopkeeper NPC is there to be
// talked to, and the shop trades whether or not one is.
type Shop struct {
	// What's for sale, each at its Item.Value. These are templates,
	// never handed over themselves -- a buyer gets a copy (see Take) --
	// so the stock never runs out.
	Stock []*Item
}

// DefaultPlayerMoney is what a new player starts with. Keep it in sync
// with the column default in sql/schema.sql.
const DefaultPlayerMoney = 10

// FormatMoney renders an amount of Ankh-Morpork dollars: "AM$12".
func FormatMoney(amount int) string {
	return fmt.Sprintf("AM$%d", amount)
}

// SellPrice is what a shop pays for item: half its Value, rounded
// down, but at least 1 for anything worth something. 0 means no shop
// wants it.
func SellPrice(item *Item) int {
	if item.Value <= 0 {
		return 0
	}
	return max(item.Value/2, 1)
}

// Take returns a new item for a buyer to walk out with: an independent
// copy of item, one of this shop's Stock.
func (s *Shop) Take(item *Item) *Item {
	return item.clone()
}

// ShopView is the JSON-serializable snapshot of a Shop.
type ShopView struct {
	Stock []ItemView `json:"stock"`
}

// View returns nil for a nil Shop, so it can sit behind omitempty.
func (s *Shop) View() *ShopView {
	if s == nil {
		return nil
	}
	return &ShopView{Stock: itemViews(s.Stock)}
}
