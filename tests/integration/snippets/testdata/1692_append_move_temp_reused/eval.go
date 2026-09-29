package main

func grow(s []int, x int) int {
	t := append(s, x)
	s = t
	return len(t)*100 + len(s)
}

func growBoxed(s []any, x any) int {
	t := append(s, x)
	s = t
	return len(t)*100 + len(s)
}

func growLoop() int {
	var s []string
	last := 0
	for i := 0; i < 5; i++ {
		t := append(s, "x")
		s = t
		last = len(t)*100 + len(s)
	}
	return last
}

func run() int {
	return grow([]int{1}, 2)*1000000 + growBoxed([]any{1}, 2)*1000 + growLoop()
}
