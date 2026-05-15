package main

import "fmt"

type Status int

func (s Status) Label() string {
	if s == 1 {
		return "active"
	}
	return "other"
}

func run() string {
	var anyStatus any = Status(1)

	labeler, ok := anyStatus.(interface{ Label() string })
	viaIface := "MISS"
	if ok {
		viaIface = labeler.Label()
	}

	st := anyStatus.(Status)
	direct := st.Label()

	_, plainInt := anyStatus.(int)

	return fmt.Sprintf("%s %s %v %v", viaIface, direct, ok, plainInt)
}
