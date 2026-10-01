package main

import (
	"fmt"
	"strings"
	"time"

	"testpkg/clock"
)

func entrypoint() string {
	d := 1500 * time.Millisecond
	parts := []string{
		clock.Direct(d),
		clock.MethodValue(d),
		clock.ViaInterface(d),
		clock.Boxed(d),
		clock.Kinds(d, 7),
		clock.Month(3),
		clock.Header("Accept", "text/plain"),
		clock.Sorted(3, 1, 2),
		clock.Mode(0o644),
		clock.Timeout.String(),
	}
	return strings.Join(parts, " | ")
}

func main() {
	fmt.Println(entrypoint())
}
