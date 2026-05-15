package main

import "fmt"

type inner struct {
	i int
	f float64
	s string
}

type holder struct {
	ptr *inner
}

func (p *inner) geti() int {
	return p.i
}

func readFloatNilLocal() (out string) {
	var p *inner
	defer func() {
		out = fmt.Sprintf("float read: %v", recover())
	}()
	v := p.f
	return fmt.Sprintf("float read: no panic %v", v)
}

func readViaHolder() (out string) {
	h := holder{}
	defer func() {
		out = fmt.Sprintf("holder read: %v", recover())
	}()
	v := h.ptr.i
	return fmt.Sprintf("holder read: no panic %d", v)
}

func readViaMethod() (out string) {
	var p *inner
	defer func() {
		out = fmt.Sprintf("method read: %v", recover())
	}()
	v := p.geti()
	return fmt.Sprintf("method read: no panic %d", v)
}

func readStringNil() (out string) {
	var p *inner
	defer func() {
		out = fmt.Sprintf("string read: %v", recover())
	}()
	v := p.s
	return fmt.Sprintf("string read: no panic %s", v)
}

func run() string {
	return readFloatNilLocal() + "\n" + readViaHolder() + "\n" + readViaMethod() + "\n" + readStringNil()
}
