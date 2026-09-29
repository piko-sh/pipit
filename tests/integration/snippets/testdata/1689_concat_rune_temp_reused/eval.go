package main

func joinTwice(s string, c rune) string {
	r := string(c)
	s = s + r
	return s + "|" + r
}

func run() string {
	acc, last := "", ""
	for _, c := range "abcd" {
		r := string(c)
		acc = acc + r
		last = r
	}
	return joinTwice("ab", 'z') + ";" + acc + "|" + last
}
