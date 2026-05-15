package main

func process(value int) {}
func EntrypointRun() int {
	total := 0
	for i := 0; i < 4; i++ {
		total += i * 2
	}
	return total
}
