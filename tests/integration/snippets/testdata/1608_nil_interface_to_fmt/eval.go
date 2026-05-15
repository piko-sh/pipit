package main

import (
	"fmt"
	"strconv"
)

type box struct{ err error }

func run() string {
	var err error
	var anything any
	n, convErr := strconv.Atoi("7")
	b := box{}
	return fmt.Sprint(err, " ", anything, " ", n, " ", convErr, " ", b.err) + "|" +
		fmt.Sprintf("%v %d %s", nil, 1, err) + "|" +
		fmt.Sprintln("d", err, anything)
}
