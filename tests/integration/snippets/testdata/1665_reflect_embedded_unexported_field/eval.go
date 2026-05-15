package main

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type hidden struct {
	N int
}

type Named struct {
	hidden
	int
	Shown string
}

var anonymous struct {
	int
}

func describe(field reflect.StructField) string {
	return fmt.Sprintf("%s/%q/%v", field.Name, field.PkgPath, field.Anonymous)
}

func run() string {
	out := ""

	anonymousType := reflect.TypeOf(anonymous)
	out += fmt.Sprintf("anon:%d:%s", anonymousType.NumField(), describe(anonymousType.Field(0)))

	anonymousValue := reflect.ValueOf(&anonymous).Elem()
	out += fmt.Sprintf(" set=%v iface=%v", anonymousValue.Field(0).CanSet(), anonymousValue.Field(0).CanInterface())

	if _, found := anonymousType.FieldByName("int"); !found {
		out += " lookup-missing"
	}

	named := Named{Shown: "x"}
	namedType := reflect.TypeOf(named)
	out += fmt.Sprintf(" named:%d", namedType.NumField())
	for i := range namedType.NumField() {
		out += " " + describe(namedType.Field(i))
	}

	namedValue := reflect.ValueOf(&named).Elem()
	out += fmt.Sprintf(" fields=%v,%v,%v",
		namedValue.Field(0).CanSet(), namedValue.Field(1).CanSet(), namedValue.Field(2).CanSet())

	byName := namedValue.FieldByName("hidden")
	out += fmt.Sprintf(" byname=%v/%v", byName.IsValid(), byName.CanSet())

	fieldOf := namedValue.Field
	out += fmt.Sprintf(" methodvalue=%v", fieldOf(1).CanSet())

	encoded, err := json.Marshal(named)
	out += fmt.Sprintf(" json=%s/%v", encoded, err)

	out += fmt.Sprintf(" print=%v %+v", named, named)

	return out
}
