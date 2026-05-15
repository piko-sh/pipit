package main

import "strconv"

type meta struct{ n int }

func pair(n int) (meta, bool) {
	if n%2 == 1 {
		return meta{}, false
	}
	return meta{n: n}, true
}

func scalarPair(n int) (int, bool) {
	if n%2 == 1 {
		return 0, false
	}
	return n * 10, true
}

func assertThenCall(v any, n int) string {
	s, ok := v.(string)
	if !ok {
		return "not-string"
	}
	m, ok := pair(n)
	if !ok {
		return "no-pair:" + s
	}
	return "pair:" + strconv.Itoa(m.n) + ":" + s
}

func assertThenScalarCall(v any, n int) string {
	s, ok := v.(string)
	if !ok {
		return "not-string"
	}
	m, ok := scalarPair(n)
	if !ok {
		return "no-pair:" + s
	}
	return "pair:" + strconv.Itoa(m) + ":" + s
}

func mapThenCall(lookup map[string]int, n int) string {
	value, ok := lookup["k"]
	if !ok {
		return "no-key"
	}
	m, ok := pair(n)
	if !ok {
		return "no-pair:" + strconv.Itoa(value)
	}
	return "pair:" + strconv.Itoa(m.n) + ":" + strconv.Itoa(value)
}

func callThenAssert(v any, n int) string {
	m, ok := pair(n)
	if !ok {
		return "no-pair"
	}
	s, ok := v.(string)
	if !ok {
		return "not-string:" + strconv.Itoa(m.n)
	}
	return "both:" + strconv.Itoa(m.n) + ":" + s
}

func assignForm(v any, n int) string {
	_, ok := v.(string)
	var m meta
	m, ok = pair(n)
	if !ok {
		return "no-pair"
	}
	return "pair:" + strconv.Itoa(m.n)
}

func run() string {
	out := ""
	for n := 0; n < 2; n++ {
		out += assertThenCall("x", n) + " " + assertThenCall(3, n) + " " + assertThenScalarCall("x", n) + " "
		out += mapThenCall(map[string]int{"k": 7}, n) + " " + callThenAssert("x", n) + " " + callThenAssert(4, n) + " "
		out += assignForm("x", n) + ";"
	}
	return out
}
