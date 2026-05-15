package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

func here(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	return fmt.Sprintf("%s:%d %v", filepath.Base(file), line, ok)
}

func inner() []string { return []string{here(0), here(1), here(9)} }

type T struct{}

func (T) Value() string      { return here(0) }
func (t *T) Pointer() string { return here(0) }

func frames() string {
	var pcs [16]uintptr
	n := runtime.Callers(1, pcs[:])
	iter := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		frame, more := iter.Next()
		if strings.HasPrefix(frame.Function, "main.") && frame.Function != "main.main" {
			out = append(out, fmt.Sprintf("%s@%d", frame.Function, frame.Line))
		}
		if !more {
			break
		}
	}
	return strings.Join(out, " ")
}

func viaFrames() string { return frames() }

func deferredCaller() (report string) {
	defer func() {
		recover()
		for skip := 1; ; skip++ {
			_, file, line, ok := runtime.Caller(skip)
			if !ok || filepath.Base(file) == "main.go" {
				report = fmt.Sprintf("panic line %d %v", line, ok)
				return
			}
		}
	}()
	var a []int
	_ = a[3]
	return ""
}

func implicitReturn() (line int) {
	func() {
		defer func() {
			_, _, line, _ = runtime.Caller(1)
		}()
	}()
	return line
}

func stack() string {
	text := string(debug.Stack())
	return fmt.Sprint(strings.HasPrefix(text, "goroutine 1 [running]:\n"), strings.Contains(text, "main.stack()"), strings.Contains(text, "main.run()"))
}

func run() string {
	var lines []string
	lines = append(lines, inner()...)
	var t T
	lines = append(lines, t.Value(), t.Pointer(), viaFrames())
	closure := func() string { return here(0) }
	lines = append(lines, closure(), deferredCaller(), fmt.Sprint(implicitReturn()), stack())
	return strings.Join(lines, "\n")
}
