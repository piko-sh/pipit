package main

import "fmt"

func run() string {
	minInt8 := int8(-128)
	minInt16 := int16(-32768)
	minInt32 := int32(-2147483648)
	negMinInt8 := -minInt8
	negMinInt16 := -minInt16
	negMinInt32 := -minInt32

	five := int8(5)
	ordinary := -five

	oneU8 := uint8(1)
	oneU16 := uint16(1)
	oneU32 := uint32(1)
	negUint8 := -oneU8
	negUint16 := -oneU16
	negUint32 := -oneU32

	fiveWide := 5
	wideNeg := -fiveWide

	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d",
		negMinInt8, negMinInt16, negMinInt32, ordinary,
		negUint8, negUint16, negUint32, wideNeg, negMinInt8+minInt8)
}
