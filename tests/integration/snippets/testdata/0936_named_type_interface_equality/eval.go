package main

type Status int

const (
	StatusUnknown Status = iota
	StatusActive
	StatusArchived
)

func (s Status) Label() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusArchived:
		return "archived"
	default:
		return "unknown"
	}
}

func run() string {
	s := StatusActive
	direct := s.Label()
	var iface interface{ Label() string } = s
	viaIface := iface.Label()
	eqDirect := s == StatusActive
	var anyVal any = s
	eqAny := anyVal == StatusActive
	return "direct=" + direct + ";iface=" + viaIface + ";eqDirect=" + boolStr(eqDirect) + ";eqAny=" + boolStr(eqAny)
}

func boolStr(b bool) string {
	if b {
		return "T"
	}
	return "F"
}
