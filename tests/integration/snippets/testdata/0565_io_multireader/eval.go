package main

import (
	"bytes"
	"io"
	"strings"
)

func run() string {
	r := io.MultiReader(
		strings.NewReader("hello, "),
		strings.NewReader("world"),
	)
	var destination bytes.Buffer
	_, _ = io.Copy(&destination, r)
	return destination.String()
}
