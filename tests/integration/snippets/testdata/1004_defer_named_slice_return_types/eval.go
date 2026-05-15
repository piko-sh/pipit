package main

import "fmt"

func intAppend() (r []int) {
	defer func() { r = append(r, 99) }()
	r = []int{1, 2}
	return r
}

func int64Append() (r []int64) {
	defer func() { r = append(r, 99) }()
	r = []int64{1, 2}
	return r
}

func uintAppend() (r []uint) {
	defer func() { r = append(r, 99) }()
	r = []uint{1, 2}
	return r
}

func byteAppend() (r []byte) {
	defer func() { r = append(r, 99) }()
	r = []byte{1, 2}
	return r
}

func floatAppend() (r []float64) {
	defer func() { r = append(r, 9.5) }()
	r = []float64{1.5, 2.5}
	return r
}

func boolAppend() (r []bool) {
	defer func() { r = append(r, true) }()
	r = []bool{false, true}
	return r
}

func stringAppend() (r []string) {
	defer func() { r = append(r, "z") }()
	r = []string{"a", "b"}
	return r
}

func stringReassign() (r []string) {
	defer func() { r = []string{"x", "y", "z"} }()
	r = []string{"a"}
	return r
}

func intReassign() (r []int) {
	defer func() { r = []int{7, 8} }()
	r = []int{1}
	return r
}

func nilAppend() (r []int) {
	defer func() { r = append(r, 7) }()
	return r
}

func run() string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v|%v|%v|%v",
		intAppend(), int64Append(), uintAppend(), byteAppend(),
		floatAppend(), boolAppend(), stringAppend(),
		stringReassign(), intReassign(), nilAppend())
}
