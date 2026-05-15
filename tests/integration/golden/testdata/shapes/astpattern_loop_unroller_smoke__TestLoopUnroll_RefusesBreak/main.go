package main

func EntrypointRun() int {
	total := 0
	for i := 0; i < 4; i++ {
		if i == 2 {
			break
		}
		total += i
	}
	return total
}
