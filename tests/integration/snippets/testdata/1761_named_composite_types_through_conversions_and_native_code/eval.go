package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

var out strings.Builder

type Path []string

func (p Path) String() string { return "path:" + strings.Join(p, "/") }

type Ints []int

func (a Ints) String() string { return fmt.Sprintf("Ints%d", len(a)) }

type Plain []int

type Words []string

type Pair [2]int

func (p Pair) String() string { return fmt.Sprintf("Pair(%d,%d)", p[0], p[1]) }

type H func(int) string

func (h H) String() string { return "H:" + h(1) }

type MyHandler func(http.ResponseWriter, *http.Request)

func (f MyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { f(w, r) }

type holder struct {
	a Ints
	h H
}

type ByLen []string

func (b ByLen) Len() int           { return len(b) }
func (b ByLen) Less(i, j int) bool { return len(b[i]) < len(b[j]) }
func (b ByLen) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

type Celsius []float64

func (c Celsius) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"C%d"`, len(c))), nil }

var gPath Path = strings.Split("g/h", "/")
var gH H = func(x int) string { return fmt.Sprint("g", x) }

func mk() Ints                  { return []int{1, 2, 3} }
func take(a Ints) any           { return a }
func takeH(h H) any             { return h }
func mkH() H                    { return func(x int) string { return fmt.Sprint("mk", x) } }
func id[T any](x T) T           { return x }
func head[S ~[]E, E any](s S) S { return s[:1] }
func set(x Ints)                { x[0] = 9 }
func setPlain(x []int)          { x[1] = 8 }
func setPath(x Path)            { x[0] = "z" }

func check(label string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(&out, "%s: panic: %v\n", label, r)
		}
	}()
	fmt.Fprintf(&out, "%s: %s\n", label, f())
}

func kind(x any) string {
	switch x.(type) {
	case Ints:
		return "Ints"
	case []int:
		return "[]int"
	case Path:
		return "Path"
	case []string:
		return "[]string"
	case H:
		return "H"
	case func(int) string:
		return "func"
	case Plain:
		return "Plain"
	case Words:
		return "Words"
	case Pair:
		return "Pair"
	case [2]int:
		return "[2]int"
	default:
		return fmt.Sprintf("other(%T)", x)
	}
}

func run() string {
	out.Reset()
	check("native result", func() string { var p Path = strings.Split("a/b", "/"); var x any = p; return kind(x) + fmt.Sprint(x) })
	check("return literal", func() string { return kind(mk()) + fmt.Sprint(any(mk())) })
	check("param literal", func() string { return kind(take([]int{1, 2})) })
	check("struct field", func() string {
		h := holder{a: []int{5}}
		h.h = func(x int) string { return "f" }
		return kind(h.a) + kind(h.h) + fmt.Sprint(h.a, h.h)
	})
	check("map literal", func() string { m := map[string]Ints{"x": []int{1}}; return kind(m["x"]) })
	check("append", func() string {
		var a Ints
		a = append(a, 1)
		b := append(Ints{1}, 2)
		return kind(a) + kind(b) + fmt.Sprint(a, b)
	})
	check("slicing", func() string { a := Ints{1, 2, 3}; return kind(a[1:]) + kind(a[:0]) })
	check("generic", func() string {
		a := Ints{1, 2}
		p := Plain{3, 4}
		return kind(id(a)) + kind(head(a)) + kind(slices.Clone(p)) + fmt.Sprint(slices.Clone(p), slices.Index(p, 4))
	})
	check("chan", func() string { ch := make(chan Ints, 1); ch <- []int{1}; v := <-ch; return kind(v) })
	check("func var", func() string {
		var h H = func(x int) string { return fmt.Sprint("v", x) }
		return kind(h) + fmt.Sprint(h)
	})
	check("func param", func() string { return kind(takeH(func(x int) string { return "p" })) })
	check("func result", func() string { return kind(mkH()) + fmt.Sprint(mkH()) })
	check("method value", func() string {
		var h H = mkH()
		var s func() string = h.String
		var anyS any = s
		return kind(anyS) + s()
	})
	check("globals", func() string { return kind(gPath) + kind(gH) + fmt.Sprint(gPath, gH) })
	check("compare", func() string {
		p := Pair{1, 2}
		ok := p == [2]int{1, 2}
		switch p {
		case [2]int{1, 2}:
			return fmt.Sprint(ok, " switch")
		}
		return fmt.Sprint(ok)
	})
	check("pair any", func() string {
		var x any = Pair{1, 2}
		var y any = [2]int{1, 2}
		return fmt.Sprint(x == y, kind(x), kind(y), x)
	})
	check("alias typed", func() string {
		a := Ints{1, 2, 3}
		set(a)
		setPlain(a)
		p := Path{"a"}
		setPath(p)
		return fmt.Sprint([]int(a), []string(p))
	})
	check("sort", func() string {
		b := ByLen{"ccc", "a", "bb"}
		sort.Sort(b)
		var x any = b
		sort.Sort(x.(sort.Interface))
		return fmt.Sprint([]string(b))
	})
	check("json", func() string {
		d, err := json.Marshal(map[string]any{"c": Celsius{1, 2}, "i": Ints{1}})
		return string(d) + fmt.Sprint(err)
	})
	check("json unmarshal", func() string {
		var w Words
		err := json.Unmarshal([]byte(`["x","y"]`), &w)
		return kind(w) + fmt.Sprint(w, err)
	})
	check("http", func() string {
		var h http.Handler = MyHandler(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hi ", r.URL.Path) })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/a/b", nil))
		rec2 := httptest.NewRecorder()
		http.StripPrefix("/a", h).ServeHTTP(rec2, httptest.NewRequest("GET", "/a/c", nil))
		std := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "std") })
		var back http.Handler = std
		_, isFunc := back.(http.HandlerFunc)
		rec3 := httptest.NewRecorder()
		back.ServeHTTP(rec3, httptest.NewRequest("GET", "/", nil))
		return rec.Body.String() + "|" + rec2.Body.String() + "|" + rec3.Body.String() + fmt.Sprint(isFunc)
	})
	check("reflect method", func() string {
		v := reflect.ValueOf(Ints{1})
		return fmt.Sprint(v.NumMethod(), v.Type().NumMethod())
	})
	check("goroutines", func() string {
		var wg sync.WaitGroup
		res := make([]string, 3)
		vals := []any{Ints{1}, H(func(int) string { return "g" }), Pair{3, 4}}
		for i := range vals {
			wg.Add(1)
			go func() { defer wg.Done(); res[i] = kind(vals[i]) + fmt.Sprint(vals[i]) }()
		}
		wg.Wait()
		return strings.Join(res, ",")
	})
	check("closure capture", func() string { a := Ints{4}; f := func() any { return a }; return kind(f()) })
	check("nil named", func() string {
		var a Ints = nil
		var x any = a
		var h H = nil
		var y any = h
		return fmt.Sprint(x == nil, y == nil, kind(x), kind(y))
	})
	return out.String()
}
