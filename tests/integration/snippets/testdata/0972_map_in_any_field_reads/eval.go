package main

import "fmt"

type holder struct {
	notes [2]any
	tag   any
}

func run() string {
	h := &holder{}
	shared := map[string]int{"hits": 1}
	h.notes[0] = shared
	h.tag = map[string]int{"t": 7}

	viaField, ok := h.notes[0].(map[string]int)
	if !ok {
		return "assert failed"
	}
	viaField["hits"]++
	viaField["extra"] = 5

	viaTag, ok := h.tag.(map[string]int)
	if !ok {
		return "tag assert failed"
	}
	viaTag["t"]++
	tagView, _ := h.tag.(map[string]int)

	return fmt.Sprintf("%d %d %d %d", shared["hits"], shared["extra"], len(viaField), tagView["t"])
}
