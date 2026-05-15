package main

func captured(n int) func() int {
	buffer := make([]int64, n)
	buffer[0] = 7
	return func() int { return int(buffer[0]) }
}
func sent(n int, ch chan []uint64) {
	buffer := make([]uint64, n)
	buffer[0] = 9
	ch <- buffer
}
func EntrypointRun() int {
	ch := make(chan []uint64, 1)
	sent(2, ch)
	return captured(2)() + int((<-ch)[0])
}
