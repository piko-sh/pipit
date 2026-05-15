package main

import "fmt"

type Token string
type Level int

func run() string {
	vals := map[string]any{
		"tok": Token("abc"),
		"lvl": Level(3),
		"raw": "abc",
	}
	_, tokPlain := vals["tok"].(string)
	tok, tokNamed := vals["tok"].(Token)
	_, lvlPlain := vals["lvl"].(int)
	lvl, lvlNamed := vals["lvl"].(Level)
	_, rawNamed := vals["raw"].(Token)
	raw, rawPlain := vals["raw"].(string)

	keys := map[any]int{Token("k"): 1, "k": 2}

	return fmt.Sprintf("%v%v%v%v%v%v %s %d %s %d %d",
		tokPlain, tokNamed, lvlPlain, lvlNamed, rawNamed, rawPlain,
		string(tok), int(lvl), raw, keys[Token("k")], keys["k"])
}
