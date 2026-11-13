package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type Label string

func (l Label) String() string { return "Label:" + string(l) }

type Code int

func (c Code) MarshalText() ([]byte, error) { return []byte(fmt.Sprintf("code-%d", int(c))), nil }

type Temp float64

func (t Temp) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"%.1fC"`, float64(t))), nil }

func (t Temp) String() string { return fmt.Sprintf("%.1f degrees", float64(t)) }

type Fault struct{ Op string }

func (f *Fault) Error() string { return "fault in " + f.Op }

type Secret struct{ Value string }

func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

type Point struct{ X, Y int }

type Wrapper struct {
	Name  string
	Label Label
	Temp  Temp
}

func logWith(handler func(*bytes.Buffer) slog.Handler) string {
	var buf bytes.Buffer
	logger := slog.New(handler(&buf))
	logger.Info("event",
		"label", Label("x"),
		"code", Code(7),
		"temp", Temp(21.5),
		"err", &Fault{Op: "read"},
		"plainErr", errors.New("plain"),
		"secret", Secret{Value: "pw"},
		"point", Point{1, 2},
		"wrapper", Wrapper{Name: "w", Label: "y", Temp: 3},
		slog.Any("any", Label("z")),
		slog.Group("g", "inner", Label("in")),
	)
	logger.With("with", Label("w")).Warn("second", "n", 1)
	return buf.String()
}

var out strings.Builder

func run() string {
	strip := func(s string) string {
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			if i := strings.Index(line, "level="); i >= 0 {
				line = line[i:]
			}
			if i := strings.Index(line, `"level"`); i >= 0 {
				line = "{" + line[i:]
			}
			out = append(out, line)
		}
		return strings.Join(out, "\n")
	}
	fmt.Fprintln(&out, strip(logWith(func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) })))
	fmt.Fprintln(&out, strip(logWith(func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) })))
	return out.String()
}
