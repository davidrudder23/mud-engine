// Package combattest provides a scripted combat.Rand for tests.
package combattest

// Rolls returns its scripted values in order from IntN. Once they run
// out it returns n-1, the worst roll there is -- every attack misses
// and nothing crits -- so a test only scripts the rolls it cares about,
// and doesn't break if the rules start rolling in a different order.
type Rolls struct {
	values []int
}

func NewRolls(values ...int) *Rolls {
	return &Rolls{values: values}
}

func (r *Rolls) IntN(n int) int {
	if len(r.values) == 0 {
		return n - 1
	}
	v := r.values[0]
	r.values = r.values[1:]
	return v
}
