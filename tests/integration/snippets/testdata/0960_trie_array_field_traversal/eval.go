package main

import "fmt"

type trieNode struct {
	children [8]*trieNode
	terminal bool
	hits     int
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
	return fmt.Sprintf("%d %d %v", found, countNodes(root), swappedIn)
}
