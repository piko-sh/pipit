package main

import "fmt"

type token struct {
	kind  int
	value int
	price float64
	live  bool
	tag   string
}

type scanner struct {
	tokens []token
	pos    int
}

func callArgAndSwitch(s *scanner) int {
	op := s.tokens[s.pos]
	total := classify(op.kind)
	switch op.kind {
	case 1:
		total += op.value
	case 2:
		total -= op.value
	}
	return total
}

func classify(kind int) int {
	return kind * 10
}

func writesThroughLocal(s *scanner) int {
	t := s.tokens[0]
	t.value = 4242
	return t.value + s.tokens[0].value
}

func stringFieldConsumer(s *scanner) string {
	t := s.tokens[0]
	return t.tag
}

func mixedKinds(s *scanner) string {
	t := s.tokens[1]
	return fmt.Sprintf("%d %.1f %v", t.kind, t.price, t.live)
}

func shadowed(s *scanner) int {
	t := s.tokens[0]
	first := t.kind
	{
		t := s.tokens[1]
		first += t.kind * 100
	}
	return first
}

func boundsPanic(s *scanner) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = "panicked"
		}
	}()
	t := s.tokens[len(s.tokens)]
	return fmt.Sprint(t.kind)
}

func plainSliceVar(tokens []token, i int) int {
	t := tokens[i]
	return t.kind*1000 + t.value
}

func run() string {
	s := &scanner{
		tokens: []token{
			{kind: 1, value: 10, price: 1.5, live: true, tag: "alpha"},
			{kind: 2, value: 20, price: 2.5, live: false, tag: "beta"},
			{kind: 3, value: 30, price: 3.5, live: true, tag: "gamma"},
		},
		pos: 1,
	}

	callSwitch := callArgAndSwitch(s)
	writes := writesThroughLocal(s)
	tag := stringFieldConsumer(s)
	mixed := mixedKinds(s)
	shadow := shadowed(s)
	panicked := boundsPanic(s)
	plain := plainSliceVar(s.tokens, 2)

	return fmt.Sprintf("%d | %d | %s | %s | %d | %s | %d",
		callSwitch, writes, tag, mixed, shadow, panicked, plain)
}
