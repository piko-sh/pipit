package main

import "fmt"

type myErrorer interface {
	Errorf(format string, args ...interface{})
}

type localT struct {
	failures []string
}

func (t *localT) Errorf(format string, args ...interface{}) {
	t.failures = append(t.failures, fmt.Sprintf(format, args...))
}

func callErrorf(t myErrorer, message string) {
	t.Errorf("got: %s", message)
}

func run() int {
	t := &localT{}
	callErrorf(t, "hello")
	return len(t.failures[0])
}
