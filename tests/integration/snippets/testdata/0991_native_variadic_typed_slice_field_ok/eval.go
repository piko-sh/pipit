package main

import (
	"fmt"
	"strings"
)

type box struct {
	ints  []int
	strs  []string
	bytes []byte
}

func pikoVariadic(xs ...interface{}) int {
	n := 0
	for _, x := range xs {
		if x != nil {
			n++
		}
	}
	return n
}

func run() string {
	b := box{ints: []int{1, 2, 3}, strs: []string{"a", "b"}, bytes: []byte("hi")}

	viaLocal := b.ints
	local := fmt.Sprintf("%v", viaLocal)

	var asAny any = b.ints
	anyForm := fmt.Sprintf("%v", asAny)

	pikoCount := pikoVariadic(b.ints, b.strs, 42)

	joined := strings.Join(b.strs, "-")

	plain := []byte("xy")
	plainForm := fmt.Sprintf("%v", plain)

	return fmt.Sprintf("%s|%s|%d|%s|%s|%d", local, anyForm, pikoCount, joined, plainForm, len(b.bytes))
}
