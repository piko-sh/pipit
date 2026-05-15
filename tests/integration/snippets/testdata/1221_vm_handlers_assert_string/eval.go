package main

func run() string {
	var x any = "hello"
	return x.(string)
}
