package main

type Leaf struct {
	V int
	S string
}

type Mid struct{ Leaf }

type Top struct {
	Mid
	Tag string
}

func run() int {
	top := Top{V: 7, S: "seven", Tag: "t"}
	if top.Mid.Leaf.V != top.V || top.Mid.Leaf.S != top.S {
		return -1
	}
	return top.V + len(top.S) + len(top.Tag)
}
