package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
)

type Rec struct {
	Name string
	Tags []string
	N    int
}

func (r Rec) String() string { return "Rec:" + r.Name }

type Names []string

func (n Names) String() string { return strings.Join(n, "+") }

type Index map[string]int

func (i Index) String() string { return fmt.Sprint("Index", len(i)) }

type Grid [2]int

func (g Grid) String() string { return "Grid" }

type Label string

func (l Label) String() string { return "Label:" + string(l) }

type Count int

func (c Count) String() string { return "Count" }

type Level float64

func (l Level) String() string { return "Level" }

type Code int

func (c Code) Error() string { return fmt.Sprintf("code %d", int(c)) }

type Errs []string

func (e Errs) Error() string { return strings.Join(e, ";") }

type T struct {
	R  Rec
	NS Names
	IX Index
	G  Grid
	L  Label
	C  Count
	V  Level
}

type Item struct {
	XMLName xml.Name `xml:"item"`
	Name    string   `xml:"name"`
	L       Label    `xml:"label"`
}

var out strings.Builder

func check(label string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(&out, label, "PANIC", r)
		}
	}()
	fmt.Fprintln(&out, label, f())
}

func run() string {
	check("json-roundtrip-T", func() string {
		t := T{R: Rec{"a", []string{"x"}, 1}, NS: Names{"p", "q"}, IX: Index{"k": 2}, G: Grid{3, 4}, L: "lab", C: 5, V: 1.5}
		b, err := json.Marshal(t)
		if err != nil {
			return err.Error()
		}
		var u T
		err = json.Unmarshal(b, &u)
		runtime.GC()
		return fmt.Sprint(string(b), " ", err, " ", reflect.DeepEqual(t, u), " ", u.R.Name, u.R.Tags, []string(u.NS), u.IX["k"], [2]int(u.G), string(u.L), int(u.C), float64(u.V))
	})
	check("json-unmarshal-named", func() string {
		var ns Names
		var ix Index
		var g Grid
		var l Label
		var c Count
		e1 := json.Unmarshal([]byte(`["a","b"]`), &ns)
		e2 := json.Unmarshal([]byte(`{"z":9}`), &ix)
		e3 := json.Unmarshal([]byte(`[7,8]`), &g)
		e4 := json.Unmarshal([]byte(`"s"`), &l)
		e5 := json.Unmarshal([]byte(`3`), &c)
		runtime.GC()
		return fmt.Sprint(e1, e2, e3, e4, e5, " ", []string(ns), ix["z"], [2]int(g), string(l), int(c))
	})
	check("json-indent", func() string {
		b, err := json.MarshalIndent(Names{"a"}, "", " ")
		return fmt.Sprint(string(b), err)
	})
	check("gob", func() string {
		var buf bytes.Buffer
		in := T{R: Rec{"g", []string{"y"}, 2}, NS: Names{"m"}, IX: Index{"q": 1}, G: Grid{1, 2}, L: "gl", C: 4, V: 2.5}
		if err := gob.NewEncoder(&buf).Encode(in); err != nil {
			return "enc " + err.Error()
		}
		var out T
		if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
			return "dec " + err.Error()
		}
		return fmt.Sprint(reflect.DeepEqual(in, out), " ", out.R.Name, []string(out.NS), out.IX["q"], string(out.L))
	})
	check("xml", func() string {
		b, err := xml.Marshal(Item{Name: "n", L: "x"})
		if err != nil {
			return "enc " + err.Error()
		}
		var out Item
		err = xml.Unmarshal(b, &out)
		return fmt.Sprint(string(b), " ", err, " ", out.Name, string(out.L))
	})
	check("slices-maps", func() string {
		ns := Names{"b", "a"}
		c := slices.Clone(ns)
		slices.Sort(c)
		i := slices.Index(ns, "a")
		ix := Index{"a": 1, "b": 2}
		keys := slices.Sorted(maps.Keys(ix))
		cl := maps.Clone(ix)
		return fmt.Sprintf("%T %v %v %d %v %T %d %v", c, []string(c), []string(ns), i, keys, cl, len(cl), slices.Equal(ns, Names{"b", "a"}))
	})
	check("deepequal", func() string {
		return fmt.Sprint(reflect.DeepEqual(Names{"a"}, Names{"a"}), reflect.DeepEqual(Names{"a"}, []string{"a"}),
			reflect.DeepEqual(Label("x"), Label("x")), reflect.DeepEqual(Label("x"), "x"), reflect.DeepEqual(Rec{Name: "a"}, Rec{Name: "a"}),
			reflect.DeepEqual(Index{"a": 1}, Index{"a": 1}), reflect.DeepEqual(Grid{1}, Grid{1}))
	})
	check("errors", func() string {
		w := fmt.Errorf("w: %w", Code(3))
		var c Code
		ok := errors.As(w, &c)
		w2 := fmt.Errorf("w2: %w", Errs{"a", "b"})
		var e Errs
		ok2 := errors.As(w2, &e)
		return fmt.Sprint(errors.Is(w, Code(3)), errors.Is(w, Code(4)), ok, int(c), ok2, len(e), w2)
	})
	check("sort", func() string {
		ns := Names{"c", "a", "b"}
		sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
		rs := []Rec{{Name: "z", N: 2}, {Name: "y", N: 1}}
		sort.Slice(rs, func(i, j int) bool { return rs[i].N < rs[j].N })
		sort.Strings(ns)
		return fmt.Sprint([]string(ns), rs[0].Name, sort.SliceIsSorted(ns, func(i, j int) bool { return ns[i] < ns[j] }))
	})
	check("fmt-pointers", func() string {
		r := &Rec{Name: "p"}
		ns := &Names{"x"}
		l := new(Label)
		*l = "y"
		return fmt.Sprintf("%v %s %+v %v %v %T %T %T", r, r, r, ns, *l, r, ns, l)
	})
	check("fmt-values", func() string {
		return fmt.Sprintf("%v|%v|%v|%v|%v|%v|%d|%v|%x", Rec{Name: "a"}, Names{"a", "b"}, Index{"a": 1}, Grid{}, Label("l"), Count(1), Count(1), Level(1), Label("hi"))
	})
	check("fmt-T", func() string {
		return fmt.Sprintf("%T %T %T %T %T %T %T", Rec{}, Names{}, Index{}, Grid{}, Label(""), Count(0), Level(0))
	})
	check("any-identity", func() string {
		r := &Rec{Name: "id"}
		var a any = r
		m := map[any]string{Names{"k"}[0]: "x", Label("k"): "label", Count(1): "count", Grid{1, 2}: "grid"}
		return fmt.Sprint(a == any(r), m[Label("k")], m[Count(1)], m[Grid{1, 2}], m["k"], len(m))
	})
	check("reflect", func() string {
		v := reflect.ValueOf(&Rec{Name: "r"}).Elem()
		v.FieldByName("N").SetInt(4)
		ns := reflect.ValueOf(Names{"a"})
		return fmt.Sprint(v.Type(), " ", v.NumField(), " ", v.FieldByName("N").Int(), " ", ns.Type(), " ", ns.Len(), " ", reflect.TypeOf(Label("")).Kind(), " ", reflect.TypeOf(Count(0)).Name())
	})
	check("json-into-interface-field", func() string {
		var m map[string]any
		b, _ := json.Marshal(map[string]any{"r": Rec{Name: "i"}, "n": Names{"z"}})
		err := json.Unmarshal(b, &m)
		return fmt.Sprint(string(b), err, len(m))
	})
	check("stringer-in-container-json", func() string {
		b, err := json.Marshal([]Label{"a", "b"})
		return fmt.Sprint(string(b), err)
	})
	return out.String()
}
