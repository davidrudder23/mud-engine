package commands

import "strings"

// MaxFuzzyDistance is the largest edit distance LLMCommandRunner ever
// tolerates between a typed verb and an alias; see AllowedDistance for
// the per-alias limit.
const MaxFuzzyDistance = 2

// AllowedDistance is how many edits a typed verb may be from alias and
// still fuzzy-match it, by the alias's letter count (spaces in a
// multi-word alias don't count): 0 -- exact only -- for 1-3 letters,
// 1 for 4, and MaxFuzzyDistance for 5 or more. Short aliases get less
// slack because almost any short word is within a couple of edits of
// them: "hi", "no", and "n" are all within 2 of "go".
func AllowedDistance(alias string) int {
	letters := len([]rune(strings.Join(strings.Fields(alias), "")))
	switch {
	case letters <= 3:
		return 0
	case letters == 4:
		return 1
	default:
		return MaxFuzzyDistance
	}
}

// Levenshtein is the edit distance between a and b: the fewest
// single-rune insertions, deletions, or substitutions turning one into
// the other.
func Levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// FuzzyMatchAlias looks for an alias within distance edits of line's
// leading words -- as many words as the alias has, so "pcik up sword"
// is compared against "pick up" and "lok around" against "look" --
// case-insensitively, never going past the alias's own
// AllowedDistance. On a match it returns line rewritten to start
// with that alias instead ("pick up sword"), so the command's own
// argument parsing works on it unchanged.
func FuzzyMatchAlias(line string, aliases []string, distance int) (string, bool) {
	words := strings.Fields(line)
	for _, alias := range aliases {
		n := len(strings.Fields(alias))
		if n == 0 || len(words) < n {
			continue
		}
		typed := strings.Join(words[:n], " ")
		d := Levenshtein(strings.ToLower(typed), strings.ToLower(alias))
		if d <= distance && d <= AllowedDistance(alias) {
			return strings.Join(append([]string{alias}, words[n:]...), " "), true
		}
	}
	return "", false
}
