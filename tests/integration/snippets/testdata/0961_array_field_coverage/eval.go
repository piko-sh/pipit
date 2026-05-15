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

func insertOne(root *trieNode, word string) {
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

func scalarAndLabelArrays() string {
	h := &holder{}
	for i := range h.slots {
		h.slots[i] = i * 11
	}
	h.labels[0] = "a"
	h.labels[2] = h.labels[0] + "z"
	sum := 0
	for _, v := range h.slots {
		sum += v
	}
	return fmt.Sprintf("%d %s %s", sum, h.labels[0], h.labels[2])
}

func mixedWrites() string {
	h := &holder{}
	h.mixed[0] = 42
	h.mixed[1] = "str"
	n, okInt := h.mixed[0].(int)
	s, _ := h.mixed[1].(string)
	return fmt.Sprintf("%d,%v,%s", n, okInt, s)
}

func wholeCopy() string {
	h := &holder{}
	h.slots[1] = 11
	whole := h.slots
	whole[0] = 999
	return fmt.Sprintf("%d %d %d", whole[0], whole[1], h.slots[0])
}

func varIndexWriteThrough() string {
	root := &trieNode{}
	slot := 3
	root.children[slot] = &trieNode{}
	child := root.children[slot]
	child.terminal = true
	return fmt.Sprint(child.terminal)
}

func run() string {
	root := &trieNode{}
	insertOne(root, "cab")
	single := root.children[2] != nil
	return fmt.Sprintf("%v | %s | %s | %s | %s",
		single, scalarAndLabelArrays(), mixedWrites(), wholeCopy(), varIndexWriteThrough())
}
