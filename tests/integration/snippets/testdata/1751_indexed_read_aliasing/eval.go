package main

import (
	"fmt"
	"sort"
)

type item struct {
	name string
	rank int
}

type byRank []*item

func (s byRank) Len() int           { return len(s) }
func (s byRank) Less(i, j int) bool { return s[i].rank < s[j].rank }
func (s byRank) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func show(items []*item) string {
	out := ""
	for _, it := range items {
		out += fmt.Sprintf("%s:%d ", it.name, it.rank)
	}
	return out
}

func pick(items []*item, i int) *item {
	return items[i]
}

func keep(it *item, items []*item) string {
	items[0] = &item{name: "replaced", rank: -1}
	return it.name
}

func deferred(items []*item) (result string) {
	defer func(it *item) { result = it.name }(items[0])
	items[0] = &item{name: "later", rank: 0}
	return ""
}

func run() string {
	out := ""
	items := []*item{{"a", 3}, {"b", 1}, {"c", 2}}
	items[0], items[1] = items[1], items[0]
	out += fmt.Sprintln(show(items))

	items[0], items[1], items[2] = items[1], items[2], items[0]
	out += fmt.Sprintln(show(items))

	first := items[0]
	items[0] = &item{name: "z", rank: 9}
	out += fmt.Sprintln(first.name, items[0].name)

	got := pick(items, 1)
	items[1] = nil
	out += fmt.Sprintln(got.name, items[1] == nil)

	items = []*item{{"x", 1}, {"y", 2}}
	out += fmt.Sprintln(keep(items[0], items), items[0].name)
	out += fmt.Sprintln(deferred(items), items[0].name)

	maps := []map[string]int{{"one": 1}, {"two": 2}}
	maps[0], maps[1] = maps[1], maps[0]
	out += fmt.Sprintln(maps[0]["two"], maps[1]["one"])

	slices := [][]int{{1}, {2, 2}, {3, 3, 3}}
	slices[0], slices[1], slices[2] = slices[2], slices[0], slices[1]
	out += fmt.Sprintln(slices)

	funcs := []func() string{func() string { return "f" }, func() string { return "g" }}
	funcs[0], funcs[1] = funcs[1], funcs[0]
	out += fmt.Sprintln(funcs[0](), funcs[1]())

	var arr [3]*item
	arr[0], arr[1], arr[2] = &item{"p", 1}, &item{"q", 2}, &item{"r", 3}
	arr[0], arr[2] = arr[2], arr[0]
	out += fmt.Sprintln(arr[0].name, arr[1].name, arr[2].name)

	ranked := byRank{{"d", 4}, {"a", 1}, {"c", 3}, {"b", 2}, {"e", 5}, {"a2", 1}}
	sort.Sort(ranked)
	out += fmt.Sprintln(show(ranked))
	sort.Sort(sort.Reverse(ranked))
	out += fmt.Sprintln(show(ranked))

	return out
}
