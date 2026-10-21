package structfields

import "sync"

type Closure struct {
	cells    []*int
	function *int
	extra    any // want `licencecheck closure-fields: field Closure.extra is not classified in licencecheck.json`
}

type Gone struct { // want `licencecheck closure-fields: licencecheck.json classifies Gone.removed, which no longer exists`
	kept int
}

type Unlisted struct{ anything any }

type Base struct{ p *int }

type List[T any] struct{ items []T }

type Pair[A, B any] struct {
	a A
	b B
}

type Embeds struct {
	*Base
	sync.Mutex
	List[int]
	Pair[int, string]
	other *int // want `licencecheck closure-fields: field Embeds.other is not classified in licencecheck.json`
}
