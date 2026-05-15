package main

import (
	"fmt"
	"strconv"
	"strings"
)

type ID [4]byte

func (id ID) String() string {
	parts := make([]string, 0, len(id))
	for _, b := range id {
		parts = append(parts, strconv.Itoa(int(b)))
	}
	return "id(" + strings.Join(parts, "-") + ")"
}

type Path []string

func (p Path) String() string { return "path:" + strings.Join(p, "/") }

type Tags map[string]int

func (t Tags) String() string { return "tags:" + strconv.Itoa(len(t)) }

type Code int

func (c Code) String() string { return "code:" + strconv.Itoa(int(c)) }

func run() string {
	id := ID{1, 2, 3, 4}
	path := Path{"a", "b"}
	tags := Tags{"x": 1}

	plain := [4]byte{1, 2, 3, 4}

	return fmt.Sprintf("%s|%v|%q|%s|%s|%v|%v|%d",
		id, id, id, path, tags, plain, Code(7), Code(7))
}
