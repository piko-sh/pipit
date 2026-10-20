package main

import "fmt"

type ints []int

type list []int

func (l list) grown() int {
	l = append(l, 9)
	return len(l)
}

func (l list) size() int { return len(l) }

func sizer(l list) func() int { return l.size }

func run() string {
	var out []string
	add := func(label string, values ...any) { out = append(out, label+"="+fmt.Sprint(values...)) }

	var c1, c2 []int
	c1 = append(c1, 1)
	d1 := c1
	d1 = append(d1, 2)
	c2 = append(c2, 1)
	d2 := c2
	c2 = append(c2, 2)
	add("copy", len(c1), len(d1), len(d2), len(c2))

	var p1, p2 []int
	p1 = append(p1, 1)
	q1 := (p1)
	q1 = append(q1, 2)
	p2 = append(p2, 1)
	q2 := (p2)
	p2 = append(p2, 2)
	add("paren", len(p1), len(q1), len(q2), len(p2))

	var b1, b2 []int
	b1 = append(b1, 1)
	a1 := ints(b1)
	a1 = append(a1, 2)
	b2 = append(b2, 1)
	a2 := ints(b2)
	b2 = append(b2, 2)
	add("conversion", len(b1), len(a1), len(a2), len(b2))

	var n1, n2 []int
	n1 = append(n1, 1)
	m1 := ints(([]int)(ints(n1)))
	m1 = append(m1, 2)
	n2 = append(n2, 1)
	m2 := ints(([]int)(ints(n2)))
	n2 = append(n2, 2)
	add("nested", len(n1), len(m1), len(m2), len(n2))

	var s1, s2 []string
	s1 = append(s1, "x")
	t1 := []string(s1)
	t1 = append(t1, "y")
	s2 = append(s2, "x")
	t2 := []string(s2)
	s2 = append(s2, "y")
	add("sameType", len(s1), len(t1), len(t2), len(s2))

	var i1 []int
	i1 = append(i1, 1)
	kept := any(i1)
	i1 = append(i1, 2)
	add("interface", len(kept.([]int)), len(i1))

	var held any = make([]int, 1, 4)
	switch v := held.(type) {
	case []int:
		v = append(v, 2)
		add("typeSwitch", len(held.([]int)), len(v))
	}

	var r list
	r = append(r, 1)
	grown := r.grown()
	add("receiver", len(r), grown)

	var mv list
	mv = append(mv, 1)
	size := mv.size
	mv = append(mv, 2)
	add("methodValue", size(), len(mv))

	long := sizer(list{1, 2, 3})
	short := sizer(list{1})
	add("escapingMethodValue", long(), short())

	return fmt.Sprint(out)
}
