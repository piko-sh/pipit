package main

import (
	"bytes"
	"io"
	"strings"
)

func run() string {
	r := io.LimitReader(strings.NewReader("hello, world"), 5)
	var destination bytes.Buffer
	_, _ = io.Copy(&destination, r)
	return destination.String()
}
