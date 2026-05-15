package main

import "fmt"

type box struct{ n int }

func run() string {
	a, b := &box{n: 1}, &box{n: 2}
	p := a
	saved := p
	var any1 interface{} = 10
	seen := any1
	s := box{n: 5}
	copied := s
	f := func() {
		p = b
		any1 = "str"
		s.n = 6
	}
	f()
	return fmt.Sprint(saved.n, " ", p.n, " ", seen, " ", any1, " ", copied.n, " ", s.n)
}
