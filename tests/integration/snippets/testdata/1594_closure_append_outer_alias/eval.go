package main

import "fmt"

func run() string {
	out := make([]byte, 0, 8)
	saved := out
	f := func() {
		out = append(out, 'x')
	}
	f()
	saved = append(saved, 'z')
	return fmt.Sprint(string(out), " ", string(saved), " ", len(out), cap(out))
}
