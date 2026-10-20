package main

import (
	"fmt"
	"slices"
	"strings"
)

type wide struct {
	Name, Group, Note, Extra string
	Score                    [12]int64
}

func run() string {
	scores := map[string]wide{}
	names := make([]string, 0, 30)
	for i := range 30 {
		name := fmt.Sprintf("n%02d", (i*7)%30)
		names = append(names, name)
		scores[name] = wide{Name: name, Score: [12]int64{int64((i * 13) % 17)}}
	}
	slices.SortFunc(names, func(a, b string) int {
		left, right := scores[a], scores[b]
		if left.Score[0] != right.Score[0] {
			return int(left.Score[0] - right.Score[0])
		}
		return strings.Compare(a, b)
	})
	index := slices.IndexFunc(names, func(name string) bool {
		w := scores[name]
		w.Note = "visited"
		scores[name] = w
		return w.Score[0] == 9
	})
	visited := 0
	for _, w := range scores {
		if w.Note == "visited" {
			visited++
		}
	}
	return fmt.Sprint(names[0], names[29], index, visited)
}
