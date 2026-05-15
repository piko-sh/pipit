package main

import (
	"errors"
	"fmt"
	"strings"
)

type Loc struct {
	Register uint8
	Kind     uint8
}

type selector struct {
	location Loc
	hasHint  bool
	hint     uint8
}

type Compiler struct{ calls int }

func (c *Compiler) detailed(name string) (selector, error) {
	if name == "" {
		return selector{}, errors.New("empty")
	}
	return selector{location: Loc{Register: 4, Kind: 3}, hasHint: name == "hinted", hint: 9}, nil
}

func (c *Compiler) fromLocation(ctx any, expr any, location Loc, receiver ...uint8) (int, error) {
	c.calls++
	if len(receiver) > 0 {
		return int(location.Register) + int(receiver[0]) + len(receiver), nil
	}
	return int(location.Register), nil
}

func (c *Compiler) selectorCall(ctx any, expr any, name string) (int, error) {
	sel, err := c.detailed(name)
	if err != nil {
		return 0, err
	}
	if sel.hasHint {
		return c.fromLocation(ctx, expr, sel.location, sel.hint)
	}
	return c.fromLocation(ctx, expr, sel.location)
}

func (c *Compiler) assigned(name string) (int, error) {
	sel, err := c.detailed(name)
	if err != nil {
		return 0, err
	}
	local := sel.hint
	v, err := c.fromLocation(nil, nil, sel.location, local, 1, 2)
	return v * 10, err
}

func (c *Compiler) spread(name string) (int, error) {
	sel, err := c.detailed(name)
	if err != nil {
		return 0, err
	}
	tail := []uint8{sel.hint, 5}
	return c.fromLocation(nil, nil, sel.location, tail...)
}

func words(prefix string, parts ...string) (string, int) {
	return prefix + fmt.Sprint(parts), len(parts)
}

func viaReturn(prefix string) (string, int) { return words(prefix, "a", "b") }

var out strings.Builder

func run() string {
	c := &Compiler{}
	v, err := c.selectorCall(nil, "e", "hinted")
	fmt.Fprintln(&out, v, err)
	v, err = c.selectorCall(nil, "e", "plain")
	fmt.Fprintln(&out, v, err)
	v, err = c.selectorCall(nil, "e", "")
	fmt.Fprintln(&out, v, err)
	v, err = c.assigned("hinted")
	fmt.Fprintln(&out, v, err)
	v, err = c.spread("hinted")
	fmt.Fprintln(&out, v, err)
	text, count := viaReturn("p")
	fmt.Fprintln(&out, text, count)
	fmt.Fprintln(&out, c.calls)
	return out.String()
}
