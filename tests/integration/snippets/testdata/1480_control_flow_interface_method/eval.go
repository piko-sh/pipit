package main

type Getter interface{ Get() int }

type Box struct{ V int }

func (b Box) Get() int { return b.V }

func run() int {
	var g Getter = Box{V: 42}
	return g.Get()
}
