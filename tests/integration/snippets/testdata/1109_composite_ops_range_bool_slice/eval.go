package main

func run() int {
	count := 0
	for _, v := range []bool{true, false, true} {
		if v {
			count++
		}
	}
	return count
}
