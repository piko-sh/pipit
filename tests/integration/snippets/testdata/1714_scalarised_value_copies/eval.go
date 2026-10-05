package main

import "fmt"

type octet byte
type bytes []byte
type view struct {
	data   bytes
	offset int
}
type embedded struct{ view }
type arrayView struct {
	data   [3]octet
	offset int
}

func (v view) at(index int) byte       { return v.data[v.offset+index] }
func (v arrayView) at(index int) octet { return v.data[v.offset+index] }
func copyFields(change bool) string {
	source := view{bytes{2, 3, 5}, 1}
	copy := source
	source.offset = 0
	source.data[1] = 11
	source.data = bytes{7, 13}
	if change {
		source.offset = 1
	}
	return fmt.Sprint(copy.offset, " ", copy.data[copy.offset], " ", source.at(0))
}
func escaped() string {
	source := view{bytes{17, 19}, 0}
	copy := source
	ptr := &copy
	fn := func() int { ptr.offset++; return copy.offset }
	source.offset = 1
	return fmt.Sprint(fn(), " ", source.offset, " ", copy.at(0))
}
func arrays() string {
	source := arrayView{[3]octet{23, 29, 31}, 1}
	copy := source
	backing := source.data[:]
	backing[1] = 37
	return fmt.Sprint(copy.at(0), " ", source.at(0))
}
func indirect() string {
	source := embedded{view{bytes{41, 43}, 0}}
	copy := source
	source.offset = 1
	var dynamic interface{ at(int) byte } = copy.view
	return fmt.Sprint(copy.at(0), " ", dynamic.at(0), " ", source.at(0))
}
func deferred() (result string) {
	source := view{bytes{47, 53}, 0}
	copy := source
	defer func(v view) { result = fmt.Sprint(v.at(0), " ", source.at(0)) }(copy)
	source.offset = 1
	return
}
func run() string {
	return fmt.Sprint(copyFields(false), "; ", copyFields(true), "; ", escaped(), "; ", arrays(), "; ", indirect(), "; ", deferred())
}
