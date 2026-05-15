package main

import "fmt"

type Bytes = []byte
type Duration = int64
type Inner struct{ V int }
type Alias = Inner

func (i Inner) Double() int { return i.V * 2 }

func takesAliasedSlice(b Bytes) int  { return len(b) }
func takesRawSlice(b []byte) int     { return len(b) }
func takesAliasedStruct(a Alias) int { return a.Double() }

var globalAliased Alias

func run() string {
	raw := []byte("hello")
	var aliased Bytes = raw

	lengths := fmt.Sprintf("%d%d%d%d",
		takesAliasedSlice(raw), takesRawSlice(aliased),
		takesAliasedSlice(aliased), takesRawSlice(raw))

	methods := fmt.Sprintf("%d,%d", takesAliasedStruct(Alias{V: 21}), globalAliased.Double())

	var d Duration = 7
	return lengths + " " + methods + " " + fmt.Sprintf("%T %T %T", aliased, d, Alias{})
}
