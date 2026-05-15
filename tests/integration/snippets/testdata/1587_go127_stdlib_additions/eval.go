package main

import (
	"bytes"
	"fmt"
	"hash/maphash"
	"math/big"
	"strings"
	"unicode"
)

func run() string {
	beforeString, afterString, foundString := strings.CutLast("a/b/c", "/")
	beforeBytes, afterBytes, foundBytes := bytes.CutLast([]byte("x.y.z"), []byte("."))

	rounded := make([]string, 0, 4)
	for _, mode := range []big.RoundingMode{big.Ceil, big.Floor, big.Round, big.Trunc} {
		value := new(big.Float).SetPrec(4).SetMode(mode)
		value.SetFloat64(2.53)
		rounded = append(rounded, value.Text('g', 4))
	}

	scripts := 0
	for _, table := range []*unicode.RangeTable{
		unicode.Garay, unicode.Kirat_Rai, unicode.Ol_Onal,
		unicode.Sunuwar, unicode.Todhri, unicode.Tulu_Tigalari,
	} {
		if table != nil && (len(table.R16) > 0 || len(table.R32) > 0) {
			scripts++
		}
	}

	seed := maphash.MakeSeed()
	first := new(maphash.Hash)
	first.SetSeed(seed)
	maphash.WriteComparable(first, "k")
	second := new(maphash.Hash)
	second.SetSeed(seed)
	maphash.WriteComparable(second, "k")

	return fmt.Sprintf("%s,%s,%v|%s,%s,%v|%v|%d|%v",
		beforeString, afterString, foundString,
		beforeBytes, afterBytes, foundBytes,
		rounded, scripts,
		first.Sum64() == second.Sum64())
}
