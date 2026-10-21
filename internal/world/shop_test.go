package world

import "testing"

func TestFormatMoney(t *testing.T) {
	if got := FormatMoney(12); got != "AM$12" {
		t.Errorf("FormatMoney(12) = %q", got)
	}
}

func TestSellPrice(t *testing.T) {
	for value, want := range map[int]int{0: 0, 1: 1, 2: 1, 3: 1, 4: 2, 15: 7, 20: 10} {
		if got := SellPrice(&Item{Value: value}); got != want {
			t.Errorf("SellPrice(value %d) = %d, want %d", value, got, want)
		}
	}
}

func TestShopTakeCopiesTheStock(t *testing.T) {
	stock := &Item{ID: "sword", Name: "a sword", Value: 10, Weapon: &Weapon{Damage: 4}}
	shop := &Shop{Stock: []*Item{stock}}

	bought := shop.Take(stock)
	if bought == stock || bought.Weapon == stock.Weapon {
		t.Fatal("Take handed over the stock itself")
	}
	if bought.Name != "a sword" || bought.Value != 10 || bought.Weapon.Damage != 4 {
		t.Errorf("bought = %+v", bought)
	}
	if len(shop.Stock) != 1 {
		t.Errorf("stock = %d", len(shop.Stock))
	}
}

func TestRoomWithAShopOffersItsCommands(t *testing.T) {
	room := &Room{ID: "shop", Shop: &Shop{}}
	if got := room.GetCommands(); len(got) != 4 || got[1] != "browse" || got[2] != "buy" || got[3] != "sell" {
		t.Errorf("commands = %q", got)
	}
}
