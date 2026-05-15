package main

type L0 struct {
	V int
	S string
}

type L1 struct{ L0 }
type L2 struct{ L1 }
type L3 struct{ L2 }
type L4 struct{ L3 }

func run() int {
	deep := L4{V: 9, S: "deep"}
	if deep.L3.L2.L1.L0.V != deep.V || deep.L3.L2.L1.L0.S != deep.S {
		return -1
	}
	return deep.V + len(deep.S)
}
