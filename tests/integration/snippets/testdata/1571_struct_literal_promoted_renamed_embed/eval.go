package main

type hidden struct{ Secret int }

type withMethod struct{ N int }

func (w withMethod) Double() int { return w.N * 2 }

type Holder struct {
	hidden
	withMethod
	Tag string
}

func run() int {
	h := Holder{Secret: 3, N: 4, Tag: "x"}
	if h.hidden.Secret != h.Secret || h.withMethod.N != h.N {
		return -1
	}
	return h.Secret + h.Double() + len(h.Tag)
}
