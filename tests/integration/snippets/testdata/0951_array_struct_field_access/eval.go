package main

import "fmt"

type trieNode struct {
	children [8]*trieNode
	terminal bool
	hits     int
}

type holder struct {
	slots  [4]int
	labels [3]string
	mixed  [2]any
}

func insert(root *trieNode, word string) {
	node := root
	for i := 0; i < len(word); i++ {
		slot := int(word[i]-'a') % 8
		if node.children[slot] == nil {
			node.children[slot] = &trieNode{}
		}
		node = node.children[slot]
	}
	node.terminal = true
	node.hits++
}

func lookup(root *trieNode, word string) bool {
	node := root
	for i := 0; i < len(word); i++ {
		slot := int(word[i]-'a') % 8
		next := node.children[slot]
		if next == nil {
			return false
		}
		node = next
	}
	return node.terminal
}

func countNodes(node *trieNode) int {
	if node == nil {
		return 0
	}
	total := 1
	for i := range node.children {
		total += countNodes(node.children[i])
	}
	return total
}

func scalarArrays() string {
	h := &holder{}
	for i := range h.slots {
		h.slots[i] = i * 11
	}
	h.labels[0] = "a"
	h.labels[2] = h.labels[0] + "z"
	h.mixed[0] = 42
	h.mixed[1] = "str"
	whole := h.slots
	whole[0] = 999
	sum := 0
	for _, v := range h.slots {
		sum += v
	}
	s, _ := h.mixed[1].(string)
	return fmt.Sprintf("%d %d %s %s %v %s", sum, whole[0], h.labels[0], h.labels[2], h.mixed[0], s)
}

func boundsProbe() (out string) {
	h := &holder{}
	index := 5
	defer func() {
		if recover() != nil {
			out = "panicked"
		}
	}()
	_ = h.slots[index]
	return "unreachable"
}

func run() string {
	root := &trieNode{}
	words := []string{"cab", "cad", "face", "fade", "bead", "bad", "cab"}
	for _, w := range words {
		insert(root, w)
	}
	found := 0
	for _, w := range []string{"cab", "cad", "face", "fade", "bead", "bad", "dead", "ca", "c"} {
		if lookup(root, w) {
			found++
		}
	}
	replaced := root.children[2]
	root.children[2] = &trieNode{terminal: true}
	swappedIn := lookup(root, "c")
	root.children[2] = replaced
	a := scalarArrays()
	b := boundsProbe()
	return fmt.Sprintf("%d %d %v %s %s", found, countNodes(root), swappedIn, a, b)
}
