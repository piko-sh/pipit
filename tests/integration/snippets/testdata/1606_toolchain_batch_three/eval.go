package main

import "fmt"

type holder struct {
	ok bool
	v  int
}

func pair() (int, string) { return 3, "three" }

func sum[S ~[]E, E int | float64](s S) E {
	var t E
	for _, x := range s {
		t += x
	}
	return t
}

func clone[S ~[]E, E any](s S) S {
	out := make(S, len(s))
	copy(out, s)
	return out
}

func run() string {
	a, b := func(i int) (int, int) { return i, i * 2 }(4)
	f := pair
	g := func() (int, string) { return f() }
	x, y := g()
	arr := [3]int{}
	vals := []int{7, 8, 9}
	for arr[0], arr[1] = range vals {
	}
	m := map[string]int{"k": 5}
	h := holder{}
	h.v, h.ok = m["k"]
	var xi, yi any = uint64(3), uint64(1) << 63
	masked := xi.(uint64) &^ yi.(uint64)
	var zi any = 5
	neg := -zi.(int)
	return fmt.Sprint(a, b, " ", x, y, " ", arr, " ", h, " ", masked, neg, " ", sum(vals), " ", clone(vals))
}
