package main

import "fmt"

func intElemWrite() (r []int) {
	defer func() { r[0] = 42 }()
	r = []int{1, 2}
	return r
}

func int64ElemWrite() (r []int64) {
	defer func() { r[0] = 42 }()
	r = []int64{1, 2}
	return r
}

func uintElemWrite() (r []uint) {
	defer func() { r[0] = 42 }()
	r = []uint{1, 2}
	return r
}

func byteElemWrite() (r []byte) {
	defer func() { r[0] = 42 }()
	r = []byte{1, 2}
	return r
}

func floatElemWrite() (r []float64) {
	defer func() { r[1] = 9.5 }()
	r = []float64{1.5, 2.5}
	return r
}

func boolElemWrite() (r []bool) {
	defer func() { r[0] = true }()
	r = []bool{false, false}
	return r
}

func stringElemWrite() (r []string) {
	defer func() { r[0] = "X" }()
	r = []string{"a", "b"}
	return r
}

func growThenElemWrite() (r []int) {
	defer func() {
		r = append(r, 3)
		r[0] = 100
	}()
	r = []int{1, 2}
	return r
}

func readOnlyDefer() (r []int) {
	defer func() { _ = len(r) }()
	r = []int{1, 2, 3}
	return r
}

func run() string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v|%v|%v",
		intElemWrite(), int64ElemWrite(), uintElemWrite(), byteElemWrite(),
		floatElemWrite(), boolElemWrite(), stringElemWrite(),
		growThenElemWrite(), readOnlyDefer())
}
