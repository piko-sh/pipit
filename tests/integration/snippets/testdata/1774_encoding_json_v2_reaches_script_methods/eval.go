package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"
)

type Celsius float64

func (c Celsius) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"%gC"`, float64(c))), nil }

func (c *Celsius) UnmarshalJSON(b []byte) error {
	var v float64
	_, err := fmt.Sscanf(string(b), `"%gC"`, &v)
	*c = Celsius(v)
	return err
}

type Level int

func (l Level) MarshalText() ([]byte, error) { return []byte(strings.Repeat("*", int(l))), nil }

func (l *Level) UnmarshalText(b []byte) error {
	*l = Level(len(b))
	return nil
}

type Reading struct {
	T     Celsius  `json:"t"`
	Level Level    `json:"level"`
	Tags  []string `json:"tags"`
	Note  string   `json:"note,omitzero"`
}

var out strings.Builder

func run() string {
	encoded, err := json.Marshal(Reading{T: 21.5, Level: 3})
	fmt.Fprintln(&out, string(encoded), err)
	var buf bytes.Buffer
	err = json.MarshalWrite(&buf, map[string]Celsius{"a": 1.5})
	fmt.Fprintln(&out, buf.String(), err)
	encoded, err = json.Marshal(Celsius(2), jsontext.Multiline(true))
	fmt.Fprintln(&out, string(encoded), err)
	var c Celsius
	err = json.Unmarshal([]byte(`"8C"`), &c)
	fmt.Fprintln(&out, float64(c), err)
	var l Level
	err = json.Unmarshal([]byte(`"****"`), &l)
	fmt.Fprintln(&out, int(l), err)
	var plain struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	err = json.UnmarshalRead(strings.NewReader(`{"a":1,"b":"x"}`), &plain)
	fmt.Fprintln(&out, plain.A, plain.B, err)
	enc := jsontext.NewEncoder(&buf)
	buf.Reset()
	err = json.MarshalEncode(enc, []Level{1, 2})
	fmt.Fprintln(&out, strings.TrimSpace(buf.String()), err)
	return out.String()
}
