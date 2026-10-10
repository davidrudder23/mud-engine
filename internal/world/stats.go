package world

// Stats are what something needs to fight: an enemy NPC (authored in
// its JSON) or a player (persisted in Postgres). See the combat package
// for what each stat does.
type Stats struct {
	Strength     int
	Dexterity    int
	Luck         int
	HitPoints    int
	MaxHitPoints int
	Stamina      int
	MaxStamina   int
}

// DefaultPlayerStats are what a new player starts with. Keep these in
// sync with the column defaults in sql/schema.sql.
func DefaultPlayerStats() Stats {
	return Stats{
		Strength: 5, Dexterity: 5, Luck: 5,
		HitPoints: 20, MaxHitPoints: 20,
		Stamina: 10, MaxStamina: 10,
	}
}

// Dead reports whether hit points are at or below zero.
func (s *Stats) Dead() bool {
	return s.HitPoints <= 0
}

// Restore refills hit points and stamina to their maximums.
func (s *Stats) Restore() {
	s.HitPoints = s.MaxHitPoints
	s.Stamina = s.MaxStamina
}

// Condition describes hit points in words, the way others see them.
func (s *Stats) Condition() string {
	switch {
	case s.HitPoints >= s.MaxHitPoints:
		return "unhurt"
	case s.HitPoints*2 > s.MaxHitPoints:
		return "wounded"
	default:
		return "badly wounded"
	}
}

// StatsView is the JSON-serializable snapshot of Stats.
type StatsView struct {
	Strength     int `json:"strength"`
	Dexterity    int `json:"dexterity"`
	Luck         int `json:"luck"`
	HitPoints    int `json:"hit_points"`
	MaxHitPoints int `json:"max_hit_points"`
	Stamina      int `json:"stamina"`
	MaxStamina   int `json:"max_stamina"`
}

// View returns nil for nil Stats, so it can sit behind omitempty.
func (s *Stats) View() *StatsView {
	if s == nil {
		return nil
	}
	return &StatsView{
		Strength: s.Strength, Dexterity: s.Dexterity, Luck: s.Luck,
		HitPoints: s.HitPoints, MaxHitPoints: s.MaxHitPoints,
		Stamina: s.Stamina, MaxStamina: s.MaxStamina,
	}
}

// clone returns an independent copy (nil stays nil).
func (s *Stats) clone() *Stats {
	if s == nil {
		return nil
	}
	copied := *s
	return &copied
}
