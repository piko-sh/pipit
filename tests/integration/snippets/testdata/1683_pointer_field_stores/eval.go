package main

import (
	"runtime"
	"strconv"
)

type node struct {
	key      int
	next     *node
	previous *node
}

type list struct {
	head *list2
	tail *list2
	size int
}

type list2 struct {
	value int
	link  *list2
}

type cache struct {
	head *node
	tail *node
}

type wrapper struct {
	inner cache
	label string
}

type holder struct {
	fn  func() int
	ch  chan int
	m   map[string]int
	any any
}

func (c *cache) pushFront(n *node) {
	n.next = c.head
	n.previous = nil
	if c.head != nil {
		c.head.previous = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *cache) unlink(n *node) {
	if n.previous != nil {
		n.previous.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.previous = n.previous
	} else {
		c.tail = n.previous
	}
	n.next = nil
	n.previous = nil
}

func (c *cache) keys() string {
	out := ""
	for n := c.head; n != nil; n = n.next {
		out += strconv.Itoa(n.key) + ","
	}
	return out
}

func (c *cache) reverseKeys() string {
	out := ""
	for n := c.tail; n != nil; n = n.previous {
		out += strconv.Itoa(n.key) + ","
	}
	return out
}

func buildList(n int) *list {
	l := &list{}
	for i := 0; i < n; i++ {
		cell := &list2{value: i}
		cell.link = l.head
		l.head = cell
		if l.tail == nil {
			l.tail = cell
		}
		l.size++
	}
	return l
}

func sumList(l *list) int {
	total := 0
	for cell := l.head; cell != nil; cell = cell.link {
		total += cell.value
	}
	return total
}

func storeThroughInterface(target any, n *node) {
	if c, ok := target.(*cache); ok {
		c.head = n
	}
}

func storeIntoEmbedded(w *wrapper, n *node) {
	w.inner.head = n
	w.inner.tail = n
}

func storeDirectKinds(h *holder) {
	h.fn = func() int { return 7 }
	h.ch = make(chan int, 1)
	h.m = map[string]int{"k": 3}
	h.any = h
	h.ch <- 5
}

func run() string {
	c := &cache{}
	nodes := make([]*node, 0, 6)
	for i := 0; i < 6; i++ {
		n := &node{key: i}
		nodes = append(nodes, n)
		c.pushFront(n)
		if i%2 == 0 {
			runtime.GC()
		}
	}
	out := c.keys() + "|" + c.reverseKeys()
	c.unlink(nodes[3])
	c.unlink(nodes[5])
	c.unlink(nodes[0])
	runtime.GC()
	out += "|" + c.keys() + "|" + c.reverseKeys()
	c.pushFront(nodes[3])
	out += "|" + c.keys()

	l := buildList(10)
	runtime.GC()
	out += "|" + strconv.Itoa(sumList(l)) + "," + strconv.Itoa(l.size) + "," + strconv.Itoa(l.tail.value)

	var typedNil *node
	c.head = typedNil
	c.tail = nil
	out += "|" + strconv.FormatBool(c.head == nil) + strconv.FormatBool(c.tail == nil)

	storeThroughInterface(c, nodes[1])
	out += "|" + strconv.Itoa(c.head.key)

	w := &wrapper{label: "w"}
	storeIntoEmbedded(w, nodes[2])
	out += "|" + strconv.Itoa(w.inner.head.key) + strconv.Itoa(w.inner.tail.key) + w.label

	h := &holder{}
	storeDirectKinds(h)
	runtime.GC()
	out += "|" + strconv.Itoa(h.fn()) + strconv.Itoa(<-h.ch) + strconv.Itoa(h.m["k"]) + strconv.FormatBool(h.any == any(h))

	keeper := &node{key: 100}
	{
		temp := &node{key: 200}
		keeper.next = temp
	}
	runtime.GC()
	out += "|" + strconv.Itoa(keeper.next.key)
	return out
}
