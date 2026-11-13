package exh

type Kind uint8

const (
	KindNone Kind = iota
	KindData
	KindCell
)

func all(k Kind) int {
	switch k {
	case KindNone:
		return 0
	case KindData:
		return 1
	case KindCell:
		return 2
	}
	return -1
}

func justified(k Kind) int {
	switch k {
	case KindData:
		return 1
	default:
		return 0
	}
}

func missing(k Kind) int {
	switch k { // want `licencecheck exhaustive-switch: switch on Kind in exh.missing does not name KindCell, and its default is not justified`
	case KindNone:
		return 0
	case KindData:
		return 1
	default:
		return -1
	}
}

func converted(k Kind) int {
	switch k { // want `licencecheck exhaustive-switch: switch on Kind in exh.converted does not name KindCell, KindData, KindNone`
	case Kind(0):
		return 0
	}
	switch {
	case k == KindData:
		return 1
	}
	switch n := int(k); n {
	case 1:
		return n
	}
	return -1
}
