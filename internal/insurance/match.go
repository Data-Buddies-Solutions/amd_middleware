package insurance

import (
	"slices"
	"strings"
	"unicode"
)

const (
	exactTokenPoints = 5
	fuzzyTokenPoints = 4
)

var fillerWords = map[string]bool{
	"a": true, "an": true, "the": true, "my": true, "i": true, "im": true, "have": true, "has": true,
	"it": true, "its": true, "is": true, "insurance": true, "plan": true, "card": true, "through": true,
	"with": true, "called": true, "says": true, "from": true, "of": true, "and": true, "uh": true,
	"um": true, "coverage": true, "policy": true, "one": true, "that": true, "this": true, "just": true,
	"regular": true,
}

var genericWords = map[string]bool{
	"health": true, "healthcare": true, "care": true, "medical": true, "vision": true, "hmo": true,
	"ppo": true, "epo": true, "pos": true, "medicare": true, "medicaid": true, "advantage": true,
	"network": true, "florida": true, "select": true, "plus": true, "choice": true, "tier": true,
}

var ambiguousSpellings = map[string]bool{"medicade": true}

var planTypes = map[string]bool{"hmo": true, "ppo": true, "epo": true, "pos": true}

var programs = []string{"medicare", "medicaid"}

func tokens(s string) []string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "’", "")
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	kept := []string{}
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "i" && i+1 < len(words) && words[i+1] == "care" {
			word = "icare"
			i++
		}
		if !fillerWords[word] {
			kept = append(kept, word)
		}
	}
	return kept
}

func callerWords(heard []string, vocabulary map[string]bool) []string {
	kept := []string{}
	for _, h := range heard {
		if ambiguousSpellings[h] {
			continue
		}
		if vocabulary[h] || genericWords[h] {
			kept = append(kept, h)
			continue
		}
		var spellings []string
		for g := range genericWords {
			if closeSpelling(h, g) {
				spellings = append(spellings, g)
			}
		}
		if len(spellings) == 1 {
			kept = append(kept, spellings[0])
			continue
		}
		if len(spellings) > 1 {
			continue
		}
		for v := range vocabulary {
			if tokenPoints(h, v) > 0 {
				kept = append(kept, h)
				break
			}
		}
	}
	return kept
}

func namesProgram(heard []string) bool {
	program := false
	for _, h := range heard {
		if !genericWords[h] {
			return false
		}
		program = program || slices.Contains(programs, h)
	}
	return program
}

func exactPlan(list planList, coverage string, heard []string) (plan, bool) {
	for _, p := range list.Plans {
		if p.Coverage == coverage && p.hasName(heard) {
			return p, true
		}
	}
	return plan{}, false
}

func bestCandidates(list planList, coverage string, heard []string) []plan {
	var found []plan
	top := 0
	for _, p := range list.Plans {
		if p.Coverage != coverage || contradictsPlanType(heard, p) {
			continue
		}
		score := 0
		for _, name := range p.nameTokens {
			score = max(score, nameScore(heard, name, list.vocabulary[coverage]))
		}
		if score == 0 || score < top {
			continue
		}
		if score > top {
			top = score
			found = nil
		}
		found = append(found, p)
	}
	return found
}

func nameScore(heard, name []string, vocabulary map[string]bool) int {
	if len(heard) == 0 {
		return 0
	}
	score := 0
	heardMatched := 0
	specific := false
	for _, h := range heard {
		best := 0
		for _, n := range name {
			points := tokenPoints(h, n)
			if points > 0 && !genericWords[h] && !genericWords[n] && !allDigits(h) {
				specific = true
			}
			best = max(best, points)
		}
		if best > 0 {
			heardMatched++
			if !genericWords[h] {
				score += best
			}
			continue
		}
		if contradicts(h, vocabulary) {
			return 0
		}
		if genericWords[h] {
			heardMatched++
		}
	}
	nameMatched := 0
	for _, n := range name {
		for _, h := range heard {
			if tokenPoints(h, n) > 0 {
				nameMatched++
				break
			}
		}
	}
	full := nameMatched == len(name)
	leads := !genericWords[name[0]] && slices.ContainsFunc(heard, func(h string) bool { return tokenPoints(h, name[0]) > 0 })
	fragment := heardMatched == len(heard) && leads
	if !specific || (!full && !fragment) {
		return 0
	}
	return score
}

func contradictsPlanType(heard []string, p plan) bool {
	if len(p.planTypes) == 0 {
		return false
	}
	for _, h := range heard {
		if planTypes[h] && !p.planTypes[h] {
			return true
		}
	}
	return false
}

func allDigits(word string) bool {
	return strings.IndexFunc(word, func(r rune) bool { return !unicode.IsDigit(r) }) == -1
}

func contradicts(word string, vocabulary map[string]bool) bool {
	return vocabulary[word] && (!genericWords[word] || slices.Contains(programs, word))
}

func tokenPoints(heard, name string) int {
	if heard == name {
		return exactTokenPoints
	}
	if genericWords[heard] || genericWords[name] {
		return 0
	}
	if closeSpelling(heard, name) {
		return fuzzyTokenPoints
	}
	return 0
}

func closeSpelling(heard, name string) bool {
	a, b := []rune(heard), []rune(name)
	shorter := min(len(a), len(b))
	distance := editDistance(a, b)
	return (shorter >= 5 && distance <= 1) || (shorter >= 9 && distance <= 2)
}

func editDistance(a, b []rune) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
