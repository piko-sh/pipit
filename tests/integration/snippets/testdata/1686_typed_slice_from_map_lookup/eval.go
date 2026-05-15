package main

import (
	"sort"
	"strconv"
)

type words []string

func joinInts(xs []int) string {
	out := ""
	for _, x := range xs {
		out += strconv.Itoa(x) + "."
	}
	return out
}

// lookups exercises every element bank the classifier admits, on hits and misses.
func lookups() string {
	strings := map[string][]string{"a": {"x", "y", "z"}, "b": {}}
	ints := map[string][]int{"a": {1, 2, 3}}
	floats := map[int][]float64{7: {0.5, 1.5}}
	bools := map[string][]bool{"a": {true, false, true}}
	bytes := map[string][]byte{"a": []byte("hey")}
	out := ""

	s := strings["a"]
	out += strconv.Itoa(len(s)) + s[0] + s[2] + ","
	for i, v := range s {
		out += strconv.Itoa(i) + v
	}
	out += ","

	miss := strings["nope"]
	out += strconv.Itoa(len(miss)) + strconv.FormatBool(miss == nil)
	for range miss {
		out += "never"
	}
	miss = append(miss, "grown")
	out += miss[0] + ","

	empty, ok := strings["b"]
	out += strconv.FormatBool(ok) + strconv.Itoa(len(empty)) + strconv.FormatBool(empty == nil) + ","

	is, present := ints["a"]
	out += strconv.FormatBool(present) + joinInts(is)
	is[1] = 20
	out += joinInts(ints["a"]) + ","

	fs := floats[7]
	out += strconv.FormatFloat(fs[0]+fs[1], 'f', 1, 64) + ","

	bs := bools["a"]
	count := 0
	for _, b := range bs {
		if b {
			count++
		}
	}
	out += strconv.Itoa(count) + ","

	by := bytes["a"]
	out += string(by[1]) + strconv.Itoa(len(by)) + ","

	grown := append(strings["a"], "w")
	strings["a"] = grown
	out += strconv.Itoa(len(strings["a"])) + strings["a"][3] + ","

	var assigned []string
	assigned, found := strings["a"]
	out += strconv.FormatBool(found) + strconv.Itoa(len(assigned)) + ","

	named := map[string]words{"a": {"n1", "n2"}}
	nw := named["a"]
	out += nw[1] + strconv.Itoa(len(nw))
	return out
}

// markov mirrors the text generator: candidate lists looked up by key, indexed by a
// pseudo-random position, the walk feeding the next key.
func markov(steps int) string {
	transitions := map[string][]string{
		"the":   {"quick", "lazy", "old"},
		"quick": {"brown", "red"},
		"lazy":  {"dog"},
		"old":   {"man", "dog"},
		"brown": {"fox"},
		"red":   {"fox"},
		"fox":   {"jumps"},
		"man":   {"sleeps"},
		"dog":   {"sleeps", "runs"},
	}
	keys := make([]string, 0, len(transitions))
	for k := range transitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seed := 7
	current := "the"
	out := current
	for i := 0; i < steps; i++ {
		nextCandidates, present := transitions[current]
		if !present || len(nextCandidates) == 0 {
			current = keys[seed%len(keys)]
			out += "|" + current
			continue
		}
		seed = (seed*1103515245 + 12345) % 2147483647
		current = nextCandidates[seed%len(nextCandidates)]
		out += " " + current
	}
	return out
}

func run() string {
	return lookups() + "|" + markov(12) + "|" + markov(3)
}
