package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Status int

func (s Status) String() string { return [...]string{"off", "on"}[s] }

type Code string

func (c Code) Error() string { return "code:" + string(c) }

type Money struct{ cents int }

func (m Money) String() string { return fmt.Sprintf("$%d.%02d", m.cents/100, m.cents%100) }

func (m Money) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf("%q", m.String())), nil }

type Wrapper struct {
	S Status
	M Money
	L []Status
	N map[string]Money
}

type Level int

func (l Level) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf("\"L%d\"", int(l))), nil }

func run() string {
	var lines []string
	lines = append(lines, fmt.Sprint([]Status{0, 1}, map[string]Status{"a": 1}, map[Status]int{1: 2}))
	lines = append(lines, fmt.Sprint(Wrapper{1, Money{1234}, []Status{1}, map[string]Money{"k": {5}}}, []Money{{5}}))
	lines = append(lines, fmt.Sprintf("%v %s %d %+v", []Status{1}, []Status{1}, []Status{1}, struct{ S Status }{1}))
	out, err := json.Marshal(Wrapper{1, Money{1234}, []Status{1}, map[string]Money{"k": {5}}})
	lines = append(lines, string(out)+" "+fmt.Sprint(err))
	out, _ = json.Marshal(map[string]Money{"x": {99}})
	lines = append(lines, string(out))
	out, _ = json.Marshal([]any{Money{1}, Level(3), []Level{1, 2}, map[string][]Money{"m": {{7}}}})
	lines = append(lines, string(out))
	out, _ = json.Marshal(struct {
		Inner struct{ L Level }
		P     *Money
	}{struct{ L Level }{9}, &Money{42}})
	lines = append(lines, string(out))
	errs := []error{Code("a"), errors.New("b")}
	errs = append(errs, Code("c"))
	lines = append(lines, fmt.Sprint(errs, " ", errors.Join(errs...).Error()))
	if c, ok := errs[0].(Code); ok {
		lines = append(lines, "assert "+string(c))
	} else {
		lines = append(lines, fmt.Sprintf("assert failed %T", errs[0]))
	}
	keys := []Status{1, 0, 1}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	lines = append(lines, fmt.Sprint(keys))
	var sb strings.Builder
	fmt.Fprintf(&sb, "%v|%v", []Code{"x"}, map[Code]Status{"y": 1})
	lines = append(lines, sb.String())
	return strings.Join(lines, "\n")
}
