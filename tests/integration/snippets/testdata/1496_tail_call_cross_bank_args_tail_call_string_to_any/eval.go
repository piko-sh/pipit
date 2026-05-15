package main

func accept(v any) any { return v }

func wrap(s string) any { return accept(s) }

func run() any {
	return wrap("hello")
}
