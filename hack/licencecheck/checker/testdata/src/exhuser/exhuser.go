package exhuser

import "exh"

func qualified(k exh.Kind) int {
	switch k {
	case exh.KindNone, (exh.KindData):
		return 0
	case exh.KindCell:
		return 2
	}
	return -1
}

func incomplete(k exh.Kind) int {
	switch k { // want `licencecheck exhaustive-switch: switch on Kind in exhuser.incomplete does not name KindCell`
	case exh.KindNone, exh.KindData:
		return 0
	}
	return -1
}
