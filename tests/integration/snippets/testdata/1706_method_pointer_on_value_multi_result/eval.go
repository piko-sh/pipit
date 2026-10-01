package main

type Counter struct {
	N int
}

func (c *Counter) Next() (int, bool) {
	c.N++
	return c.N, c.N < 3
}

type Wrapper struct {
	Counter
}

func run() int {
	var c Counter
	n, ok := c.Next()
	m, _ := c.Next()
	var w Wrapper
	_, _ = w.Next()
	k, more := w.Next()
	if !ok || more {
		return -1
	}
	return c.N*100 + n*10 + m + k*1000 + w.N*10000
}
