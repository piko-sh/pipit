package main

import "strconv"

type box struct {
	f    int
	name string
	next *box
}

// zeroTripInt writes a struct field into a named local inside a loop that may never run.
// A field read hoisted above the loop condition would leak s.f into the result, or fault
// when s is nil.
func zeroTripInt(s *box, n int) int {
	v := 1
	for i := 0; i < n; i++ {
		v = s.f
	}
	return v
}

func zeroTripString(s *box, n int) string {
	v := "init"
	for i := 0; i < n; i++ {
		v = s.name
	}
	return v
}

func zeroTripPointer(s *box, n int) *box {
	var v *box
	for i := 0; i < n; i++ {
		v = s.next
	}
	return v
}

// zeroTripAccumulate reads the field into a local the loop also consumes, so the local is
// live after the loop in both the zero-trip and the taken case.
func zeroTripAccumulate(s *box, n int) int {
	total := 0
	v := 100
	for i := 0; i < n; i++ {
		v = s.f
		total += v
	}
	return total*10 + v
}

// zeroTripValueReceiver repeats the shape with a struct held by value.
func zeroTripValueReceiver(s box, n int) int {
	v := -7
	for i := 0; i < n; i++ {
		v = s.f
	}
	return v
}

// joinBeforeWhile has an if/else whose join point is exactly the loop header, so the
// then-arm enters the loop through a forward jump rather than by falling through.
func joinBeforeWhile(s *box, flag bool, n int) int {
	i, total := 0, 0
	if flag {
		total = 5
	} else {
		total = 7
	}
	for i < n {
		total += s.f
		i++
	}
	return total
}

// nestedJoin repeats the join shape inside an outer loop, so the inner loop is entered
// through the forward jump on every outer iteration.
func nestedJoin(s *box, n int) int {
	total := 0
	for outer := 0; outer < n; outer++ {
		i := 0
		if outer%2 == 0 {
			total += 100
		} else {
			total += 200
		}
		for i < outer {
			total += s.f
			i++
		}
	}
	return total
}

// twoBackEdges rewrites the receiver on the path that continues past the first back-edge,
// so a read that is invariant in the shorter loop is not invariant in the whole loop.
func twoBackEdges(s, s2 *box, n int) int {
	i, total := 0, 0
	for i < n {
		total += s.f
		if i%2 == 0 {
			i++
			continue
		}
		s = s2
		i++
	}
	return total
}

func describe(p *box) string {
	if p == nil {
		return "nil"
	}
	return p.name
}

func run() string {
	leaf := &box{f: 9, name: "leaf", next: nil}
	root := &box{f: 42, name: "root", next: leaf}
	out := ""
	for _, n := range []int{0, 1, 3} {
		out += strconv.Itoa(n) + ":" +
			strconv.Itoa(zeroTripInt(root, n)) + "," +
			zeroTripString(root, n) + "," +
			describe(zeroTripPointer(root, n)) + "," +
			strconv.Itoa(zeroTripAccumulate(root, n)) + "," +
			strconv.Itoa(zeroTripValueReceiver(*root, n)) + ";"
	}
	out += "|join:" + strconv.Itoa(joinBeforeWhile(root, true, 4)) + "," + strconv.Itoa(joinBeforeWhile(root, false, 4)) +
		"," + strconv.Itoa(joinBeforeWhile(nil, true, 0)) + "," + strconv.Itoa(joinBeforeWhile(nil, false, 0)) +
		"|nested:" + strconv.Itoa(nestedJoin(root, 5)) +
		"|two:" + strconv.Itoa(twoBackEdges(leaf, root, 6)) + ";"
	// A nil receiver with a zero-trip loop must not fault.
	out += "nil:" + strconv.Itoa(zeroTripInt(nil, 0)) + "," + zeroTripString(nil, 0) + "," + describe(zeroTripPointer(nil, 0))
	return out
}
