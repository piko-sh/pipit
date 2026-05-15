package main

type Reader interface {
	Read() string
}

type Writer interface {
	Write(s string)
}

type ReadWriter interface {
	Reader
	Writer
}

type buffer struct {
	data string
}

func (b *buffer) Read() string   { return b.data }
func (b *buffer) Write(s string) { b.data = s }

func run() string {
	var rw ReadWriter = &buffer{}
	rw.Write("hello")
	return rw.Read()
}
