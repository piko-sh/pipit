package main

import "fmt"

type counters struct {
	signed   int
	unsigned uint
}

func decSignedThroughNil() (out string) {
	var p *counters
	defer func() { out = fmt.Sprintf("int dec: %v", recover()) }()
	p.signed--
	return "int dec: no panic"
}

func incUnsignedThroughNil() (out string) {
	var p *counters
	defer func() { out = fmt.Sprintf("uint inc: %v", recover()) }()
	p.unsigned++
	return "uint inc: no panic"
}

func decUnsignedThroughNil() (out string) {
	var p *counters
	defer func() { out = fmt.Sprintf("uint dec: %v", recover()) }()
	p.unsigned--
	return "uint dec: no panic"
}

func run() string {
	return decSignedThroughNil() + "\n" + incUnsignedThroughNil() + "\n" + decUnsignedThroughNil()
}
