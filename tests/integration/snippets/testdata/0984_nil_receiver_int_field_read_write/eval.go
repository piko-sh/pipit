package main

import "fmt"

type box struct {
	n int
}

func readIntThroughNil() (out string) {
	var p *box
	defer func() { out = fmt.Sprintf("int read: %v", recover()) }()
	value := p.n
	return fmt.Sprintf("int read: no panic %d", value)
}

func writeIntThroughNil() (out string) {
	var p *box
	defer func() { out = fmt.Sprintf("int write: %v", recover()) }()
	p.n = 7
	return "int write: no panic"
}

func readViaMethodThroughNil() (out string) {
	var p *box
	defer func() { out = fmt.Sprintf("method read: %v", recover()) }()
	return fmt.Sprintf("method read: no panic %d", p.doubled())
}

func (p *box) doubled() int {
	return p.n * 2
}

func run() string {
	return readIntThroughNil() + "\n" + writeIntThroughNil() + "\n" + readViaMethodThroughNil()
}
