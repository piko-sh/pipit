package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func named(skip int) string {
	pc, file, line, ok := runtime.Caller(skip + 1)
	return fmt.Sprintf("%s:%d %s %v", filepath.Base(file), line, runtime.FuncForPC(pc).Name(), ok)
}

func inner() []string { return []string{named(0), named(1), named(9)} }

type T struct{}

func (T) Value() string      { return named(0) }
func (t *T) Pointer() string { return named(0) }

func run() string {
	var lines []string
	lines = append(lines, inner()...)
	var t T
	closure := func() string { return named(0) }
	lines = append(lines, t.Value(), t.Pointer(), closure())
	pc, _, _, _ := runtime.Caller(0)
	fn := runtime.FuncForPC(pc)
	file, line := fn.FileLine(pc)
	var none *runtime.Func
	lines = append(lines, fmt.Sprint(fn == runtime.FuncForPC(pc), fn.Entry() != 0,
		filepath.Base(file), line, none.Name() == ""))
	return strings.Join(lines, "\n")
}
