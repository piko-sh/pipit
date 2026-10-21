package main

import "fmt"

type point struct {
	x, y int
}

type counter struct {
	hits  int
	total uint64
	names []string
	sizes []int
	label string
}

type node struct {
	next  *node
	value int
}

var scale = 3

var offsets = []int{10, 20, 30}

func tally(values []int) (sum int) {
	for _, v := range values {
		sum += v
	}
	return
}

func accumulate(n int) (total int) {
	total += n
	total += n
	return
}

func moved(p point, dx int) point {
	p.x += dx
	return p
}

func bump(c *counter, by uint64) {
	c.hits++
	c.total += by
}

func widen(v uint32) uint32 { return v*2654435761 + 7 }

func low8(v uint64) uint8 { return uint8(v) }

func byteAt(s string, i int) byte { return s[i] }

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func length(s string) int { return len(s) }

func lookup(m map[string]int, key string) (int, bool) {
	v, ok := m[key]
	return v, ok
}

func nextOf(n *node) *node { return n.next }

func push(dst []int, v int) []int { return append(dst, v) }

func pushThrough(dst *[]int, v int) { *dst = append(*dst, v) }

func setAt(values []int, i, v int) { values[i] = v }

func getAt(values []int, i int) int { return values[i] }

func firstTwo(values []int) []int { return values[:2] }

func half(s string) string { return s[:len(s)/2] }

func scaled(v int) int { return v * scale }

func offsetOf(i int) int { return offsets[i] }

func newNode(v int, next *node) *node { return &node{value: v, next: next} }

func remember(c *counter, name string, size int) {
	c.names = append(c.names, name)
	c.sizes = append(c.sizes, size)
	c.label = name
}

func nameAt(c *counter, i int) string { return c.names[i] }

func forget(m map[int]string, key int) { delete(m, key) }

func store(m map[int]string, key int, value string) { m[key] = value }

func swapEnds(values [4]int) [4]int {
	values[0], values[3] = values[3], values[0]
	return values
}

func divide(a, b int) (quotient, remainder int) {
	quotient = a / b
	remainder = a % b
	return
}

func classify(v int) (kind string, even bool) {
	if v%2 == 0 {
		return "even", true
	}
	if v < 0 {
		return "negative", false
	}
	return "odd", false
}

func ratio(a, b int) (r int) {
	r = a
	r = r / b
	return
}

func guarded(a, b int) (out int) {
	defer func() {
		if recover() != nil {
			out += 1000
		}
	}()
	out = 7
	out = ratio(a, b)
	return
}

type particle struct {
	x, y float64
}

func nudge(p *particle, v float64) float64 {
	p.x = v + 100
	return v
}

func ranged() string {
	particles := []particle{{x: 1, y: 2}, {x: 3, y: 4}}
	total := 0.0
	for _, p := range particles {
		total += nudge(&particles[0], p.y)
		total += p.x
	}
	return fmt.Sprint("ranged ", total, particles[0].x, "\n")
}

type ledger struct {
	entries []int
	notes   []string
}

func record(l *ledger, v int) int {
	l.entries = append(l.entries, v)
	return len(l.entries)
}

func recordTwice(l *ledger, v int) int {
	record(l, v)
	return record(l, v+1)
}

func note(l *ledger, text string) {
	l.notes = append(l.notes, text)
}

func noteLoudly(l *ledger, text string) {
	note(l, text+"!")
}

func pair(a, b int) (int, int) { return a * 10, b * 100 }

func swapped(x, y int) (int, int) { return pair(y, x) }

func isEven(n int) bool {
	if n == 0 {
		return true
	}
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 {
		return false
	}
	return isEven(n - 1)
}

func countDown(n, acc int) int {
	if n == 0 {
		return acc
	}
	return countDown(n-1, acc+n)
}

func tails() string {
	l := &ledger{}
	last := 0
	for i := 0; i < 4; i++ {
		last = recordTwice(l, i*10)
		noteLoudly(l, fmt.Sprint("n", i))
	}
	a, b := swapped(3, 4)
	return fmt.Sprint("tails ", last, l.entries, l.notes, a, b, isEven(50000), isOdd(50001), countDown(60000, 0), "\n")
}

func sums() string {
	total := 0
	for i := 0; i < 4; i++ {
		total += tally([]int{i, i + 1, i + 2})
		total += accumulate(i)
	}
	return fmt.Sprint("sums ", total, "\n")
}

func points() string {
	origin := point{x: 1, y: 2}
	trail := []point{}
	for i := 0; i < 3; i++ {
		trail = append(trail, moved(origin, i*10))
	}
	return fmt.Sprint("points ", origin, trail, "\n")
}

func scan() string {
	c := &counter{}
	state := uint32(7)
	digits, total := 0, 0
	text := "a1b22c333"
	for i := 0; i < length(text); i++ {
		state = widen(state)
		bump(c, uint64(low8(uint64(state))))
		if isDigit(byteAt(text, i)) {
			digits++
		}
		total += scaled(i) + offsetOf(i%3)
	}
	return fmt.Sprint("scan ", c.hits, c.total, state, digits, total, "\n")
}

func lookups() string {
	m := map[string]int{"a": 1, "b": 2}
	found, missing := 0, 0
	for _, key := range []string{"a", "x", "b", "y", "a"} {
		v, ok := lookup(m, key)
		if ok {
			found += v
		} else {
			missing++
		}
	}
	return fmt.Sprint("lookups ", found, missing, "\n")
}

func walk() string {
	var list *node
	for i := 0; i < 5; i++ {
		list = newNode(i, list)
	}
	walked := 0
	for n := list; n != nil; n = nextOf(n) {
		walked = walked*10 + n.value
	}
	return fmt.Sprint("walk ", walked, "\n")
}

func slices() string {
	var grown, through []int
	base := make([]int, 0, 8)
	for i := 0; i < 5; i++ {
		grown = push(grown, i)
		pushThrough(&through, i*i)
		_ = push(base, i)
	}
	setAt(grown, 1, 99)
	return fmt.Sprint("slices ", grown, through, base, len(base), getAt(grown, 1), firstTwo(grown), half("abcdef"), "\n")
}

func fields() string {
	c := &counter{}
	for i := 0; i < 3; i++ {
		remember(c, fmt.Sprint("n", i), i*2)
	}
	return fmt.Sprint("fields ", c.names, c.sizes, c.label, nameAt(c, 1), "\n")
}

func maps() string {
	names := map[int]string{}
	for i := 0; i < 4; i++ {
		store(names, i, fmt.Sprint("v", i))
	}
	for i := 0; i < 4; i += 2 {
		forget(names, i)
	}
	return fmt.Sprint("maps ", len(names), names[1], names[3], names[0] == "", "\n")
}

func arrays() string {
	ends := [4]int{1, 2, 3, 4}
	var swapped [4]int
	for i := 0; i < 3; i++ {
		swapped = swapEnds(ends)
	}
	return fmt.Sprint("arrays ", ends, swapped, "\n")
}

func results() string {
	q, r := 0, 0
	kinds := ""
	evens := 0
	for i := 1; i < 6; i++ {
		q, r = divide(17+q, i)
		kind, even := classify(i - 3)
		kinds += kind[:1]
		if even {
			evens++
		}
	}
	return fmt.Sprint("results ", q, r, kinds, evens, guarded(9, 3), guarded(9, 0), "\n")
}

func run() string {
	return sums() + points() + scan() + lookups() + walk() + slices() + fields() + maps() + arrays() + results() + ranged() + tails()
}
