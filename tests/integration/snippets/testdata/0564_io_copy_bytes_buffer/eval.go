package main

import (
	"bytes"
	"io"
)

func run() string {
	source := bytes.NewBufferString("hello, copy!")
	var destination bytes.Buffer
	_, _ = io.Copy(&destination, source)
	return destination.String()
}
