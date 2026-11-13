package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type point struct{ x, y int }

type wrapped struct{ inner *point }

type celsius float64

type label string

func (l label) String() string { return "label " + string(l) }

var kept = map[string]any{}

func describe(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%T:%v", key, m[key], m[key]))
	}
	return strings.Join(parts, " ")
}

func scalars() string {
	m := map[string]any{}
	n, f, b, s := 42, 2.5, true, "text"
	var small uint8 = 200
	var wide int64 = -1 << 40
	m["int"] = n
	m["float"] = f
	m["bool"] = b
	m["string"] = s
	m["uint8"] = small
	m["int64"] = wide
	m["rune"] = 'x'
	m["named"] = celsius(36.6)
	m["label"] = label("tag")
	m["nil"] = nil
	n, f, b, s, small = 0, 0, false, "changed", 0
	return describe(m)
}

func aggregates() string {
	m := map[string]any{}
	numbers := []int{1, 2, 3}
	p := &point{x: 1, y: 2}
	value := point{x: 3, y: 4}
	array := [2]int{5, 6}
	inner := map[int]string{1: "one"}
	box := wrapped{inner: p}
	m["slice"] = numbers
	m["pointer"] = p
	m["struct"] = value
	m["array"] = array
	m["map"] = inner
	m["wrapped"] = box
	m["func"] = len(numbers)
	numbers[0] = 9
	p.x = 7
	value.x = 8
	array[0] = 8
	inner[2] = "two"
	return fmt.Sprint(m["slice"], *m["pointer"].(*point), m["struct"], m["array"], m["map"], m["wrapped"].(wrapped).inner.x, m["func"])
}

func interfaces() string {
	m := map[string]any{}
	var err error = errors.New("failed")
	var stringer fmt.Stringer = label("held")
	var empty any = 7
	var none error
	m["error"] = err
	m["stringer"] = stringer
	m["any"] = empty
	m["no error"] = none
	named := map[string]fmt.Stringer{}
	named["a"] = label("in a named interface")
	return describe(m) + " | " + fmt.Sprint(named["a"].String(), m["no error"] == nil)
}

func intKeys() string {
	m := map[int]any{}
	for i := 0; i < 6; i++ {
		switch i % 3 {
		case 0:
			m[i] = i * 10
		case 1:
			m[i] = fmt.Sprint("s", i)
		default:
			m[i] = []int{i}
		}
	}
	m[-1] = point{x: 1}
	small := map[int8]any{}
	small[-3] = "minus three"
	wide := map[uint64]any{}
	wide[1<<63] = 1.5
	out := []string{}
	for i := -1; i < 6; i++ {
		out = append(out, fmt.Sprintf("%T:%v", m[i], m[i]))
	}
	return strings.Join(out, " ") + fmt.Sprint(" ", small[-3], wide[1<<63], len(m))
}

func overwrites() string {
	for round := 0; round < 3; round++ {
		text := fmt.Sprint("round ", round)
		kept["text"] = text
		kept["count"] = round
		kept["both"] = text + "!"
	}
	total := 0
	for round := 0; round < 50; round++ {
		local := map[string]any{}
		local["a"] = round
		local["b"] = fmt.Sprint(round)
		total += local["a"].(int) + len(local["b"].(string))
	}
	return describe(kept) + fmt.Sprint(" ", total)
}

func switched(m map[string]any) string {
	out := []string{}
	for _, key := range []string{"int", "string", "slice", "missing"} {
		switch value := m[key].(type) {
		case int:
			out = append(out, fmt.Sprint("int ", value+1))
		case string:
			out = append(out, "string "+value)
		case []int:
			out = append(out, fmt.Sprint("slice ", len(value)))
		case nil:
			out = append(out, "nil")
		}
	}
	return strings.Join(out, ",")
}

func missing(m map[string]any) string {
	_, isInt := m["missing"].(int)
	_, isAny := m["missing"].(any)
	_, isError := m["missing"].(error)
	number, isNumber := m["int"].(int)
	failure := func() (message string) {
		defer func() { message = fmt.Sprint(recover()) }()
		return fmt.Sprint(m["missing"].(int))
	}()
	held := m["missing"]
	label := "not nil"
	switch held.(type) {
	case nil:
		label = "nil"
	case any:
		label = "any"
	}
	return fmt.Sprint(isInt, isAny, isError, number, isNumber, " ", failure, " ", label, " ", held == nil)
}

func nilMap() (message string) {
	defer func() { message = fmt.Sprint(recover()) }()
	var m map[string]any
	m["a"] = 1
	return "no panic"
}

func nilIntMap() (message string) {
	defer func() { message = fmt.Sprint(recover()) }()
	var m map[int]any
	m[1] = "a"
	return "no panic"
}

func run() string {
	m := map[string]any{}
	m["int"] = 1
	m["string"] = "s"
	m["slice"] = []int{1, 2}
	return strings.Join([]string{
		scalars(),
		aggregates(),
		interfaces(),
		intKeys(),
		overwrites(),
		switched(m),
		missing(m),
		fmt.Sprint(m["int"] == any(1), m["string"] == any("s"), m["int"] == any(int64(1))),
		nilMap(),
		nilIntMap(),
	}, "\n")
}
