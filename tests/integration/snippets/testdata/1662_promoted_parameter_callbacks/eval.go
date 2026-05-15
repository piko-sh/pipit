package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type C struct {
	depth int
	names int
}

func (c *C) dispatch(n ast.Node, tag string) (string, error) {
	c.depth++
	defer func() { c.depth-- }()
	if id, ok := n.(*ast.Ident); ok {
		c.names++
		return tag + id.Name, nil
	}
	return tag + fmt.Sprintf("%T", n), nil
}

func (c *C) walk(body ast.Node, tag string) (int, error) {
	c.depth++
	defer func() { c.depth-- }()
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if _, err := c.dispatch(n, tag); err != nil {
			return false
		}
		count++
		if lit, ok := n.(*ast.FuncLit); ok {
			inner, _ := c.walk(lit.Body, tag+">")
			count += inner
			return false
		}
		return true
	})
	return count, nil
}

type holder struct {
	fn func(c *C, body ast.Node, tag string) (int, error)
}

func plain(c *C, body ast.Node, tag string) (int, error) {
	defer func() { c.depth += 0 }()
	return c.walk(body, tag)
}

var out strings.Builder

func run() string {
	src := "package p\nfunc f() {\n\tfns := []func() int{}\n\tfor i := 0; i < 3; i++ {\n\t\tfns = append(fns, func() int { return i })\n\t}\n\t_ = fns\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		panic(err)
	}
	c := &C{}
	n, err := c.walk(file, "w")
	fmt.Fprintln(&out, "compiled call:", n, err, c.depth, c.names)
	h := holder{fn: (*C).walk}
	n, err = h.fn(c, file, "m")
	fmt.Fprintln(&out, "method expression:", n, err, c.depth, c.names)
	h = holder{fn: plain}
	n, err = h.fn(c, file, "p")
	fmt.Fprintln(&out, "func field:", n, err, c.depth, c.names)
	bound := c.walk
	n, err = bound(file, "b")
	fmt.Fprintln(&out, "bound method:", n, err, c.depth, c.names)
	name, err := c.dispatch(&ast.Ident{Name: "z"}, "d")
	fmt.Fprintln(&out, "dispatch:", name, err, c.depth, c.names)
	return out.String()
}
