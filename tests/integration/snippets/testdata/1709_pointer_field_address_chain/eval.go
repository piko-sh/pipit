package main

type node struct {
	next  *node
	value int
}

func run() int {
	table := make([]*node, 3)
	for i := range 6 {
		p := &table[i%3]
		for *p != nil {
			p = &(*p).next
		}
		*p = &node{value: i}
	}
	return table[0].next.value + table[1].next.value + table[2].next.value
}
