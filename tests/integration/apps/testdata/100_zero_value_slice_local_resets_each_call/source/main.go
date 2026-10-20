package main

import (
	"fmt"
	"strings"

	"testpkg/lib"
)

func entrypoint() string {
	var parts []string
	for _, n := range []int{3, 3, 2, 4} {
		parts = append(parts, fmt.Sprint(lib.Ints(n), lib.Strings(n), lib.Floats(n), lib.Bytes(n), lib.Bools(n), lib.Records(n), lib.Joined(n)))
	}
	return strings.Join(parts, " | ")
}

func main() {
	fmt.Println(entrypoint())
}
