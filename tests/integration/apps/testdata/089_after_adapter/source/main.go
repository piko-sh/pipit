package main

import (
	"fmt"

	"testpkg/lib"
)

func entrypoint() string {
	buffer := lib.NewBuffer()
	fmt.Fprint(buffer, "fprint-here|")
	buffer.Write([]byte("direct-here"))
	return "fp-buffer=>>" + buffer.String() + "<<"
}

func main() {
	fmt.Println(entrypoint())
}
