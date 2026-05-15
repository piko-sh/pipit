package main

import "fmt"

type ID int
type Name string

func makeAnyID() any {
	id := ID(41)
	id++
	return id
}

func makeName() Name {
	n := Name("piko")
	return n
}

func viaClosure() any {
	f := func() any {
		return Name("closed")
	}
	return f()
}

func run() string {
	a := makeAnyID()
	_, plainInt := a.(int)
	id, namedID := a.(ID)

	var b any = makeName()
	_, plainStr := b.(string)
	nm, namedNm := b.(Name)

	c := viaClosure()
	_, cPlain := c.(string)
	cn, cNamed := c.(Name)

	return fmt.Sprintf("%v%v %v%v %v%v %d %s %s",
		plainInt, namedID, plainStr, namedNm, cPlain, cNamed,
		int(id), string(nm), string(cn))
}
