package main

import "fmt"

func run() string {
	chain := map[string][]string{
		"a": {"one", "two", "three"},
	}

	first := chain["a"]
	first[0] = "ONE"
	visible := chain["a"][0]

	grown := append(first, "four")
	mapLen := len(chain["a"])
	grownLen := len(grown)

	missing := chain["nope"]

	return fmt.Sprintf("%s %d %d %d %v", visible, mapLen, grownLen, len(missing), missing == nil)
}
