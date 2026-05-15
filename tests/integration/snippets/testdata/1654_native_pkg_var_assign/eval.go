package main

import (
	"fmt"
	"runtime"
	"strings"
)

func run() string {
	var lines []string
	previous := runtime.MemProfileRate
	runtime.MemProfileRate = 16 * 1024
	lines = append(lines, fmt.Sprint(runtime.MemProfileRate, runtime.MemProfileRate == 16*1024))
	runtime.MemProfileRate += 1024
	lines = append(lines, fmt.Sprint(runtime.MemProfileRate))
	runtime.MemProfileRate = previous
	lines = append(lines, fmt.Sprint(runtime.MemProfileRate == previous))
	return strings.Join(lines, "\n")
}
