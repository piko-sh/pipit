package main

import "fmt"

type ID int
type Name string
type Ratio float64
type Flag bool
type Small uint16
type Kind uint8

type holder struct {
	IDs    []ID
	Names  []Name
	Ratios []Ratio
	Flags  []Flag
	Smalls []Small
	Kinds  []Kind
}

func viaLocals(n int) *holder {
	h := &holder{}
	var ids []ID
	var names []Name
	var ratios []Ratio
	var flags []Flag
	var smalls []Small
	var kinds []Kind
	for i := 0; i < n; i++ {
		ids = append(ids, ID(i))
		names = append(names, Name("n"+fmt.Sprint(i)))
		ratios = append(ratios, Ratio(i)/2)
		flags = append(flags, Flag(i%2 == 0))
		smalls = append(smalls, Small(i+7))
		kinds = append(kinds, Kind(i+1))
	}
	h.IDs, h.Names, h.Ratios, h.Flags, h.Smalls, h.Kinds = ids, names, ratios, flags, smalls, kinds
	return h
}

func viaFields(n int) *holder {
	h := &holder{}
	for i := 0; i < n; i++ {
		h.IDs = append(h.IDs, ID(i))
		h.Names = append(h.Names, Name("f"+fmt.Sprint(i)))
		h.Ratios = append(h.Ratios, Ratio(i)/4)
		h.Flags = append(h.Flags, Flag(i%2 == 1))
		h.Smalls = append(h.Smalls, Small(i+9))
		h.Kinds = append(h.Kinds, Kind(i+3))
	}
	return h
}

func run() string {
	out := ""
	for _, h := range []*holder{viaLocals(4), viaFields(4)} {
		out += fmt.Sprintln(h.IDs, h.Names, h.Ratios, h.Flags, h.Smalls, h.Kinds)
	}
	return out
}
