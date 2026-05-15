package main

import (
	"errors"
	"fmt"
)

type point struct{ x, y int }

func recoverErr() error {
	defer func() { recover() }()
	panic("boom")
}

func recoverPtr() *point {
	defer func() { recover() }()
	panic("boom")
}

func recoverSlice() []int {
	defer func() { recover() }()
	panic("boom")
}

func recoverFloatBool() (float64, bool) {
	defer func() { recover() }()
	panic("boom")
}

func recoverAfterReturn() (string, error) {
	defer func() { recover() }()
	defer func() { panic("late") }()
	return "kept", errors.New("e")
}

func run() string {
	e := recoverErr()
	p := recoverPtr()
	s := recoverSlice()
	f, b := recoverFloatBool()
	k, ke := recoverAfterReturn()
	return fmt.Sprint(e == nil, " ", p == nil, " ", s == nil, " ", len(s), " ", f, " ", b, " ", k, " ", ke)
}
