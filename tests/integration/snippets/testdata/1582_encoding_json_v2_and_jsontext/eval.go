package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
)

type record struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Tags  []string `json:"tags,omitempty"`
}

func run() string {
	encoded, marshalErr := jsonv2.Marshal(record{Name: "alpha", Count: 2, Tags: []string{"x", "y"}})

	var decoded record
	unmarshalErr := jsonv2.Unmarshal([]byte(`{"name":"beta","count":7}`), &decoded)

	empty, emptyErr := jsonv2.Marshal(record{Name: "gamma"})

	value := jsontext.String("hello")

	quotedFromString, quoteErr := jsontext.AppendQuote(nil, "a\"b")
	quotedFromBytes, bytesErr := jsontext.AppendQuote(nil, []byte("c\td"))
	unquoted, unquoteErr := jsontext.AppendUnquote(nil, `"e\nf"`)
	formatted, formatErr := jsontext.AppendFormat(nil, `{"k":[1,2]}`)

	return fmt.Sprintf("%s|%v %v %d %v|%s|%s|%s|%s|%s|%s|%v",
		encoded, marshalErr, decoded.Name, decoded.Count, unmarshalErr,
		empty, value.String(),
		quotedFromString, quotedFromBytes, unquoted, formatted,
		emptyErr == nil && quoteErr == nil && bytesErr == nil &&
			unquoteErr == nil && formatErr == nil)
}
