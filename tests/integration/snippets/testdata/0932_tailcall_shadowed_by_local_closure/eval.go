package main

import "fmt"

func greet() string {
	return "top-level"
}

func viaClosure() string {
	greet := func() string {
		return "local-closure"
	}
	return greet()
}

func viaParam(greet func() string) string {
	return greet()
}

func run() string {
	local := viaClosure()
	param := viaParam(func() string { return "param-closure" })
	return fmt.Sprintf("%s|%s|%s", local, param, greet())
}
