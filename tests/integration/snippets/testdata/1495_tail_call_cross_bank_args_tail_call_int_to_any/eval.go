package main

func accept(v any) any { return v }

func wrap(n int) any { return accept(n) }

func run() any {
	return wrap(42)
}
