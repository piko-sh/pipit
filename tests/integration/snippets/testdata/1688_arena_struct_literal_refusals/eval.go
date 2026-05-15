package main

import "strconv"

type holder struct {
	items []int
	name  string
	link  *holder
}

var published *holder
var registry = map[string]*holder{}
var pile []*holder

// self returns its receiver, so the pointer escapes through the return.
func (h *holder) self() *holder { return h }

// publish stores its receiver in a package variable.
func (h *holder) publish() { published = h }

// register stores its receiver in a map.
func (h *holder) register() { registry[h.name] = h }

// enqueue stores its receiver in a slice.
func (h *holder) enqueue() { pile = append(pile, h) }

// capture closes over its receiver.
func (h *holder) capture() func() int { return func() int { return len(h.items) } }

// alias links the receiver to another holder, which then reaches it.
func (h *holder) alias(other *holder) { other.link = h }

func (h *holder) total() int {
	t := 0
	for _, v := range h.items {
		t += v
	}
	return t
}

func fill(seed int) []int {
	items := make([]int, 0, 8)
	for i := 0; i < 8; i++ {
		items = append(items, seed*10+i)
	}
	return items
}

func storm(seed int) int {
	total := 0
	for i := 0; i < 300; i++ {
		block := make([]int, 48)
		block[i%48] = seed + i
		total += block[i%48] + len(strconv.Itoa(total))
	}
	return total
}

func viaReturn(seed int) *holder {
	h := holder{items: fill(seed), name: "r" + strconv.Itoa(seed)}
	return h.self()
}

func viaGlobal(seed int) {
	h := holder{items: fill(seed), name: "g"}
	h.publish()
}

func viaMap(seed int) {
	h := holder{items: fill(seed), name: "m" + strconv.Itoa(seed)}
	h.register()
}

func viaSlice(seed int) {
	h := holder{items: fill(seed), name: "s"}
	h.enqueue()
}

func viaClosure(seed int) func() int {
	h := holder{items: fill(seed), name: "c"}
	return h.capture()
}

func viaFieldAddress(seed int) *[]int {
	h := holder{items: fill(seed), name: "f"}
	return &h.items
}

func viaInterface(seed int) any {
	h := holder{items: fill(seed), name: "i"}
	h.total()
	return h
}

func viaKeptAddress(seed int) *holder {
	h := holder{items: fill(seed), name: "k"}
	q := &h
	q.total()
	return q
}

func viaLink(seed int, other *holder) {
	h := holder{items: fill(seed), name: "l"}
	h.alias(other)
}

func run() string {
	out := ""
	r := viaReturn(1)
	viaGlobal(2)
	viaMap(3)
	viaMap(4)
	viaSlice(5)
	viaSlice(6)
	closure := viaClosure(7)
	fieldPointer := viaFieldAddress(8)
	boxed := viaInterface(9)
	kept := viaKeptAddress(10)
	anchor := &holder{name: "anchor"}
	viaLink(11, anchor)
	storm(1)
	storm(2)
	unboxed := boxed.(holder)
	out += strconv.Itoa(r.total()) + "," + strconv.Itoa(published.total()) + "," +
		strconv.Itoa(registry["m3"].total()) + strconv.Itoa(registry["m4"].total()) + "," +
		strconv.Itoa(pile[0].total()+pile[1].total()) + "," +
		strconv.Itoa(closure()) + "," + strconv.Itoa((*fieldPointer)[7]) + "," +
		strconv.Itoa(unboxed.total()) + "," + strconv.Itoa(kept.total()) + "," +
		strconv.Itoa(anchor.link.total()) + anchor.link.name
	return out
}
