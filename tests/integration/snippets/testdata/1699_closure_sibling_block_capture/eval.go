package main

import "fmt"

func pair() (int, int) { return 2, 3 }

func shortVar() int {
	{
		x := 1
		f := func() int { return x }
		_ = f()
	}
	y := 2
	g := func() int { return y }
	return g()
}

func varSpec() int {
	{
		var x = 1
		f := func() int { return x }
		_ = f()
	}
	var y = 2
	g := func() int { return y }
	return g()
}

func multiReturn() int {
	{
		a, _ := pair()
		f := func() int { return a + 10 }
		_ = f()
	}
	b, _ := pair()
	g := func() int { return b }
	return g()
}

func commaOk() int {
	m := map[string]int{"k": 2}
	{
		x := 1
		f := func() int { return x }
		_ = f()
	}
	v, ok := m["k"]
	g := func() int { return v }
	if !ok {
		return -1
	}
	return g()
}

func typeSwitch() int {
	var value any = 2
	{
		x := 1
		f := func() int { return x }
		_ = f()
	}
	switch v := value.(type) {
	case int:
		g := func() int { return v }
		return g()
	}
	return -1
}

func ifInit() int {
	{
		x := 1
		f := func() int { return x }
		_ = f()
	}
	if y := 2; y > 0 {
		g := func() int { return y }
		return g()
	}
	return -1
}

func slices() int {
	{
		x := []int{1}
		f := func() int { return x[0] }
		_ = f()
	}
	{
		x := []int{2}
		f := func() int { return x[0] }
		return f()
	}
}

func escapedClosureKeepsItsOwn() string {
	var first func() int
	{
		x := 1
		first = func() int { return x }
	}
	y := 2
	second := func() int { return y }
	return fmt.Sprint(first(), second(), y)
}

func oldClosureLeavesUncapturedAlone() int {
	var f func() int
	{
		x := 1
		f = func() int { return x }
	}
	z := 5
	_ = f()
	return z
}

func oldClosureLeavesCapturedAlone() int {
	var f func() int
	{
		x := 1
		f = func() int { return x }
	}
	y := 5
	g := func() int { return y }
	_ = f()
	return g() + y*100
}

func loopIterationsGetTheirOwn() string {
	pairOf := func(i int) (int, int) { return i, i }
	var short, varSpec, commaOk, multi, tswitch []func() int
	m := map[int]int{0: 0, 1: 10, 2: 20}
	for i := 0; i < 3; i++ {
		a := i
		short = append(short, func() int { return a })
		var b = i
		varSpec = append(varSpec, func() int { return b })
		v, _ := m[i]
		commaOk = append(commaOk, func() int { return v })
		p, _ := pairOf(i)
		multi = append(multi, func() int { return p })
		var boxed any = i
		switch t := boxed.(type) {
		case int:
			tswitch = append(tswitch, func() int { return t })
		}
	}
	return fmt.Sprint(callThree(short), callThree(varSpec), callThree(commaOk), callThree(multi), callThree(tswitch))
}

func callThree(fs []func() int) []int {
	return []int{fs[0](), fs[1](), fs[2]()}
}

func run() string {
	return fmt.Sprint(shortVar(), varSpec(), multiReturn(), commaOk(), typeSwitch(), ifInit(), slices(),
		escapedClosureKeepsItsOwn(), " ", oldClosureLeavesUncapturedAlone(), oldClosureLeavesCapturedAlone(),
		" ", loopIterationsGetTheirOwn())
}
