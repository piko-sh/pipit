package main

var count int32
var values []int

func resize(names []string) {
	count = int32(len(names))
	if count == 0 {
		return
	}
	values = make([]int, int(count))
}

func run() int {
	resize([]string{"a", "b", "c"})
	return len(values)
}
