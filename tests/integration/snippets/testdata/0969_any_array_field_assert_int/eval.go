package main

import "fmt"

type holder struct {
	mixed [2]any
}

func run() string {
	h := &holder{}
	h.mixed[0] = 42
	h.mixed[1] = "str"
	vInt, okInt := h.mixed[0].(int)
	vInt64, okInt64 := h.mixed[0].(int64)
	s, okStr := h.mixed[1].(string)
	return fmt.Sprintf("int=%d,%v int64=%d,%v str=%s,%v", vInt, okInt, vInt64, okInt64, s, okStr)
}
