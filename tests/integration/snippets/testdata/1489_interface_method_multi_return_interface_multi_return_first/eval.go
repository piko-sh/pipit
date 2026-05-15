package main

type Parser interface{ Parse(s string) (int, bool) }

type IntParser struct{}

func (p IntParser) Parse(s string) (int, bool) {
	if s == "ok" {
		return 10, true
	}
	return 0, false
}

func run() int {
	var p Parser = IntParser{}
	n, _ := p.Parse("ok")
	return n
}
