package main

import (
	"fmt"

	"testpkg/lib"
)

func entrypoint() string {
	buffer := lib.NewBuffer()
	fmt.Fprint(buffer, "hello world")
	return "buffer=>>" + buffer.String() + "<<"
}

func main() {
	fmt.Println(entrypoint())
}
