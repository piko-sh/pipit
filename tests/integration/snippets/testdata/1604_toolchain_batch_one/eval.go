package main

import "fmt"

type container struct{ Value string }

func pair() (int, string) { return 1, "two" }

var c = b
var a, b = pair()
var d = a + 10

func anon(int, bool) int        { return 9 }
func mixed(x int, _ string) int { return x + 1 }

func idArray(in [4]byte) (out [4]byte) {
	copy(out[:], in[:])
	return
}

func mustPanic(f func()) (recovered bool) {
	defer func() { recovered = recover() != nil }()
	f()
	return false
}

func run() string {
	var e, f = pair()
	rows := make([][]int, 2)
	index := uint64(1)
	rows[index] = []int{7}
	keyed := []container{7: {Value: "seven"}}
	arr := idArray([4]byte{1, 2, 3, 4})
	n := -1
	panicked := mustPanic(func() { _ = make([]int, n) })
	return fmt.Sprint(a, b, c, d, " ", e, f, " ", anon(1, true), mixed(1, "x"), " ", len(rows[1]), " ", len(keyed), keyed[7].Value, " ", arr[3], " ", panicked)
}
