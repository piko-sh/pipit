package main

func weigh(s string, i int) int {
	b := s[i]
	n := int(b)
	return n*1000 + int(b&0x0f)
}

func run() int {
	total := 0
	s := "pipit"
	for i := 0; i < len(s); i++ {
		total += weigh(s, i)
	}
	return total
}
