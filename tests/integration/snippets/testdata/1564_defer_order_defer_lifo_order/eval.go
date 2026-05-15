package main

func f() (result string) {
	defer func() { result += "a" }()
	defer func() { result += "b" }()
	defer func() { result += "c" }()
	return ""
}

func run() string {
	return f()
}
