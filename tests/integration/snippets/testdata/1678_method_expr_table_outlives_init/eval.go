package main

import "strconv"

type loc struct {
	register uint8
	kind     int
}

type comp struct {
	depth    int
	maxDepth int
	trace    int
}

type lowering struct {
	name string
	try  func(c *comp, n int) (loc, bool, error)
}

var lowerings = []lowering{{name: "inplace", try: (*comp).tryInPlace}}

func (c *comp) note() {
	c.trace++
}

func (c *comp) tryInPlace(n int) (loc, bool, error) {
	value, err := c.descend(n)
	c.note()
	if err != nil {
		return loc{}, true, err
	}
	return value, true, nil
}

func (c *comp) descend(n int) (loc, error) {
	c.depth++
	if c.depth > c.maxDepth {
		c.maxDepth = c.depth
	}
	defer func() { c.depth-- }()
	defer c.note()
	if n == 0 {
		return loc{register: 1, kind: 2}, nil
	}
	inner, err := c.descend(n - 1)
	if err != nil {
		return loc{}, err
	}
	return loc{register: inner.register + 1, kind: inner.kind}, nil
}

func (c *comp) run(n int) string {
	for _, l := range lowerings {
		location, applied, err := l.try(c, n)
		if err != nil {
			return "error"
		}
		if applied {
			return l.name + ":" + strconv.Itoa(int(location.register)) + "/" + strconv.Itoa(location.kind)
		}
	}
	return "generic"
}

func run() string {
	out := ""
	for _, n := range []int{3, 40, 70, 130, 300, 700} {
		c := &comp{}
		out += c.run(n) + " " + strconv.Itoa(c.depth) + " " + strconv.Itoa(c.maxDepth) + " " + strconv.Itoa(c.trace) + ";"
	}
	return out
}
