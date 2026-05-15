package main

import (
	"reflect"
	"strconv"
)

type sample struct {
	weight int64
	score  float64
	flag   bool
	name   string
}

func indexAfterLen(t reflect.Type, name string) int {
	field, ok := t.FieldByName(name)
	if !ok || len(field.Index) != 1 {
		return -1
	}
	return field.Index[0]
}

func indexDirect(t reflect.Type, name string) int {
	field, _ := t.FieldByName(name)
	return field.Index[0]
}

func indexViaLocal(t reflect.Type, name string) int {
	field, _ := t.FieldByName(name)
	index := field.Index
	return index[0] + len(index)*10
}

func kindThenIndex(t reflect.Type, name string) int {
	field, _ := t.FieldByName(name)
	if field.Type.Kind() == reflect.String {
		return 100 + field.Index[0]
	}
	return field.Index[0]
}

func run() string {
	t := reflect.TypeOf(sample{})
	out := ""
	for _, name := range []string{"weight", "score", "flag", "name"} {
		out += strconv.Itoa(indexAfterLen(t, name)) + ","
		out += strconv.Itoa(indexDirect(t, name)) + ","
		out += strconv.Itoa(indexViaLocal(t, name)) + ","
		out += strconv.Itoa(kindThenIndex(t, name)) + ";"
	}
	out += strconv.Itoa(indexAfterLen(t, "missing"))
	return out
}
