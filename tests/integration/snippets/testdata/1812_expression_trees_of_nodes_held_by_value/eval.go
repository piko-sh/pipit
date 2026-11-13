package main

import "fmt"

type node interface {
	Eval(env []uint32) uint32
}

type constNode struct{ value uint32 }

func (n constNode) Eval(env []uint32) uint32 { return n.value }

type varNode struct{ index int }

func (n varNode) Eval(env []uint32) uint32 { return env[n.index] }

type addNode struct{ left, right node }

func (n addNode) Eval(env []uint32) uint32 { return n.left.Eval(env) + n.right.Eval(env) }

type mulNode struct{ left, right node }

func (n mulNode) Eval(env []uint32) uint32 { return n.left.Eval(env) * n.right.Eval(env) }

type modNode struct{ left, right node }

func (n modNode) Eval(env []uint32) uint32 {
	divisor := n.left.Eval(env)
	if divisor == 0 {
		return 0
	}
	return n.right.Eval(env) % divisor
}

type minNode struct{ left, right node }

func (n minNode) Eval(env []uint32) uint32 {
	l, r := n.left.Eval(env), n.right.Eval(env)
	if l < r {
		return l
	}
	return r
}

type ifPosNode struct{ cond, then, otherwise node }

func (n ifPosNode) Eval(env []uint32) uint32 {
	if n.cond.Eval(env) != 0 {
		return n.then.Eval(env)
	}
	return n.otherwise.Eval(env)
}

type wide interface {
	Eval(env []uint64) uint64
}

type wideConst struct{ value uint64 }

func (n wideConst) Eval(env []uint64) uint64 { return n.value }

type wideVar struct{ index int }

func (n wideVar) Eval(env []uint64) uint64 { return env[n.index] }

type wideMod struct{ left, right wide }

func (n wideMod) Eval(env []uint64) uint64 {
	divisor := n.left.Eval(env)
	if divisor == 0 {
		return 0
	}
	return n.right.Eval(env) % divisor
}

type wideMin struct{ left, right wide }

func (n wideMin) Eval(env []uint64) uint64 {
	l, r := n.left.Eval(env), n.right.Eval(env)
	if l < r {
		return l
	}
	return r
}

type wideIfPos struct{ cond, then, otherwise wide }

func (n wideIfPos) Eval(env []uint64) uint64 {
	if n.cond.Eval(env) != 0 {
		return n.then.Eval(env)
	}
	return n.otherwise.Eval(env)
}

type wideLowMod struct{ left, right wide }

func (n wideLowMod) Eval(env []uint64) uint64 {
	divisor := n.left.Eval(env)
	if divisor == 0 {
		return 0
	}
	return uint64(uint32(n.right.Eval(env) % divisor))
}

type wideQueue chan wideConst

func (q wideQueue) Eval(env []uint64) uint64 { return uint64(cap(q)) + env[2] }

type wideTable map[wideConst]bool

func (m wideTable) Eval(env []uint64) uint64 { return uint64(len(m)) + env[2] }

type counter struct{ hits int }

func (c *counter) Hits() int { return c.hits }

type reading interface{ Hits() int }

func evaluate(tree node, env []uint32) uint32 { return tree.Eval(env) }

func narrowTrees() string {
	first := []uint32{100, 200, 3, 0}
	second := []uint32{9, 4, 0, 7}
	byValue := []node{
		varNode{1},
		addNode{left: varNode{0}, right: varNode{1}},
		mulNode{left: varNode{0}, right: constNode{4000000000}},
		modNode{left: varNode{2}, right: varNode{1}},
		modNode{left: varNode{3}, right: varNode{1}},
		minNode{left: varNode{0}, right: varNode{1}},
		ifPosNode{cond: varNode{3}, then: constNode{1}, otherwise: varNode{0}},
		ifPosNode{cond: varNode{2}, then: varNode{0}, otherwise: varNode{1}},
	}
	byPointer := []node{
		&varNode{1},
		&addNode{left: &varNode{0}, right: varNode{1}},
		&minNode{left: varNode{0}, right: &constNode{50}},
		&ifPosNode{cond: &varNode{2}, then: varNode{0}, otherwise: &varNode{1}},
	}
	out := ""
	for round := 0; round < 4; round++ {
		env := first
		if round%2 == 1 {
			env = second
		}
		for _, tree := range byValue {
			out += fmt.Sprint(tree.Eval(env), evaluate(tree, env), " ")
		}
		for _, tree := range byPointer {
			out += fmt.Sprint(tree.Eval(env), " ")
		}
		first[0] += 1000
		second[1]++
	}
	return out
}

func wideTrees() string {
	env := []uint64{1 << 32, 1<<40 + 7, 3, 0}
	small := []uint64{9, 4, 3, 0}
	big := wideConst{value: 1 << 32}
	trees := []wide{
		wideIfPos{cond: big, then: wideConst{1}, otherwise: wideConst{2}},
		wideIfPos{cond: wideVar{0}, then: wideConst{1}, otherwise: wideConst{2}},
		wideIfPos{cond: wideVar{3}, then: wideConst{1}, otherwise: wideConst{2}},
		wideMod{left: big, right: wideConst{1<<33 + 5}},
		wideMod{left: wideVar{0}, right: wideVar{1}},
		wideMod{left: wideConst{1<<32 + 1}, right: wideConst{1 << 40}},
		wideMod{left: wideVar{3}, right: wideVar{1}},
		wideMin{left: big, right: wideConst{5}},
		wideMin{left: wideVar{1}, right: wideVar{0}},
		&wideMod{left: &wideMin{left: wideVar{1}, right: &wideVar{0}}, right: wideIfPos{cond: big, then: &wideVar{1}, otherwise: wideVar{2}}},
	}
	out := ""
	for round := 0; round < 3; round++ {
		for _, tree := range trees {
			out += fmt.Sprint(tree.Eval(env), tree.Eval(small), " ")
		}
	}
	return out
}

func widePointerTrees() string {
	env := []uint64{1 << 32, 1<<40 + 7, 3, 0}
	queue := make(wideQueue, 4)
	table := wideTable{wideConst{1}: true, wideConst{2}: true}
	trees := []wide{
		&wideLowMod{left: &wideConst{1<<32 | 2}, right: &wideConst{7}},
		&wideLowMod{left: &wideConst{1 << 32}, right: &wideConst{1<<33 | 5}},
		&wideLowMod{left: &wideConst{3 << 32}, right: &wideConst{1<<32 | 7}},
		&wideLowMod{left: &wideVar{3}, right: &wideVar{1}},
		&wideIfPos{cond: &wideConst{1 << 32}, then: &wideConst{1}, otherwise: &wideConst{2}},
		&wideIfPos{cond: &wideVar{0}, then: &wideVar{2}, otherwise: &wideVar{1}},
		&wideMin{left: &wideConst{9}, right: &wideConst{50}},
		&wideMin{left: queue, right: &wideConst{50}},
		&wideMin{left: &wideConst{60}, right: table},
		&wideMin{left: &wideConst{8}, right: &wideConst{6}},
	}
	out := ""
	for round := 0; round < 4; round++ {
		for _, tree := range trees {
			out += fmt.Sprint(tree.Eval(env), " ")
		}
	}
	return out
}

func readings() string {
	counters := []reading{&counter{hits: 3}, &counter{hits: 5}}
	total := 0
	for round := 0; round < 3; round++ {
		for _, each := range counters {
			total = total*10 + each.Hits()
		}
		counters[0].(*counter).hits++
	}
	return fmt.Sprint(total)
}

func run() string {
	return fmt.Sprint(narrowTrees(), "\n", wideTrees(), "\n", widePointerTrees(), "\n", readings())
}
