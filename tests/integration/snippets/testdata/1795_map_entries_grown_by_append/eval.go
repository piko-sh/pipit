package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type words []string

type label string

type index struct {
	byFirst map[string][]string
	total   int
}

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func render(m map[string][]string) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strings.Join(m[key], ","))
	}
	return strings.Join(parts, ";")
}

func grouped(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < n; i++ {
		key, value := "k"+strconv.Itoa(i%3), "v"+strconv.Itoa(i)
		m[key] = append(m[key], value)
	}
	return m
}

func computedBetween(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < n; i++ {
		key := "k" + strconv.Itoa(i%3)
		m[key] = append(m[key], "v"+strconv.Itoa(i))
	}
	return m
}

func intoAParameter(m map[string][]string, key, value string) {
	m[key] = append(m[key], value)
}

func afterEscaping(sink *[]map[string][]string, early, late string) string {
	m := map[string][]string{}
	for i := 0; i < 4; i++ {
		value := "e" + strconv.Itoa(i)
		m[early] = append(m[early], value)
	}
	*sink = append(*sink, m)
	for i := 0; i < 4; i++ {
		value := "l" + strconv.Itoa(i)
		m[late] = append(m[late], value)
	}
	return render(m)
}

func sharedBacking(key, second string) string {
	m := map[string][]string{}
	m[key] = make([]string, 1, 4)
	m[key][0] = "first"
	before := m[key]
	m[key] = append(m[key], second)
	before = append(before, "overwritten")
	return strings.Join(m[key], ",") + "|" + strings.Join(before, ",")
}

func intoANilMap() string {
	return caught(func() { intoAParameter(nil, "a", "x") })
}

func namedTypes() string {
	byLabel := map[label]words{}
	for i := 0; i < 5; i++ {
		key, value := label("n"+strconv.Itoa(i%2)), "w"+strconv.Itoa(i)
		byLabel[key] = append(byLabel[key], value)
	}
	return strings.Join(byLabel["n0"], ",") + ";" + strings.Join(byLabel["n1"], ",")
}

func namedKeys() string {
	byLabel := map[label][]string{}
	for i := 0; i < 5; i++ {
		key, value := label("n"+strconv.Itoa(i%2)), "w"+strconv.Itoa(i)
		byLabel[key] = append(byLabel[key], value)
	}
	return strings.Join(byLabel["n0"], ",") + ";" + strings.Join(byLabel["n1"], ",")
}

func throughAField(n int) string {
	ix := &index{byFirst: map[string][]string{}}
	for i := 0; i < n; i++ {
		word := string(rune('a'+i%3)) + strconv.Itoa(i)
		ix.byFirst[word[:1]] = append(ix.byFirst[word[:1]], word)
		ix.total++
	}
	return render(ix.byFirst) + "#" + strconv.Itoa(ix.total)
}

func keyAndValueFromTheMap() string {
	m := map[string][]string{"a": {"b"}, "b": {"a"}}
	for i := 0; i < 3; i++ {
		m[m["a"][0]] = append(m[m["a"][0]], m["b"][len(m["b"])-1]+strconv.Itoa(i))
	}
	return render(m)
}

func otherKeysBetween() string {
	m := map[string][]string{}
	m["x"] = append(m["y"], "from-y")
	m["y"] = append(m["x"], "from-x")
	return render(m)
}

func grownLong(key string) string {
	m := map[string][]string{}
	for i := 0; i < 300; i++ {
		value := strconv.Itoa(i)
		m[key] = append(m[key], value)
	}
	return strconv.Itoa(len(m[key])) + ":" + m[key][0] + ":" + m[key][150] + ":" + m[key][299]
}

func run() string {
	var sink []map[string][]string
	parameter := map[string][]string{"kept": {"old"}}
	intoAParameter(parameter, "kept", "new")
	intoAParameter(parameter, "fresh", "only")
	results := []string{
		render(grouped(10)),
		render(computedBetween(10)),
		render(parameter),
		afterEscaping(&sink, "early", "late") + "/" + render(sink[0]),
		sharedBacking("a", "second"),
		intoANilMap(),
		namedTypes(),
		namedKeys(),
		throughAField(7),
		keyAndValueFromTheMap(),
		otherKeysBetween(),
		grownLong("only"),
	}
	return strings.Join(results, "\n")
}
