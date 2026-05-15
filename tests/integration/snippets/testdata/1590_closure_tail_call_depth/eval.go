package main

func countDown(n int, f func(int)) {
	f(n - 1)
}

func run() int {
	total := 0
	var f func(int)
	f = func(n int) {
		total++
		if n == 0 {
			return
		}
		countDown(n, f)
	}
	f(8000)
	return total
}
