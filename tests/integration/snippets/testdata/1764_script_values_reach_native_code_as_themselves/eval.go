package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

type P struct {
	Name string
	Age  int
}

func (p P) String() string { return "P(" + p.Name + ")" }

type L []int

func (l L) String() string { return fmt.Sprintf("L%d", len(l)) }

type M map[string]int

func (m M) String() string { return "M" }

type S string

func (s S) String() string { return "S:" + string(s) }

type N int

func (n N) String() string { return fmt.Sprintf("N%d", int(n)) }

type E struct{ Code int }

func (e *E) Error() string { return fmt.Sprintf("E%d", e.Code) }

type VE struct{ Code int }

func (e VE) Error() string { return fmt.Sprintf("VE%d", e.Code) }

type J struct{ X int }

func (j J) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`{"x":%d}`, j.X)), nil }

type U struct{ X int }

type W struct {
	In  J
	Out P
}

func (u *U) UnmarshalJSON(b []byte) error {
	u.X = len(b)
	return nil
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
	check("json-unmarshal-struct", func() string {
		var p P
		err := json.Unmarshal([]byte(`{"Name":"a","Age":3}`), &p)
		return fmt.Sprint(err, p.Name, p.Age)
	})
	check("json-unmarshal-slice", func() string {
		var l L
		err := json.Unmarshal([]byte(`[1,2,3]`), &l)
		return fmt.Sprint(err, len(l), []int(l))
	})
	check("json-unmarshal-map", func() string {
		var m M
		err := json.Unmarshal([]byte(`{"a":1}`), &m)
		return fmt.Sprint(err, len(m), m["a"])
	})
	check("json-unmarshal-basic", func() string {
		var s S
		err := json.Unmarshal([]byte(`"hi"`), &s)
		return fmt.Sprint(err, string(s))
	})
	check("json-unmarshal-int", func() string {
		var n N
		err := json.Unmarshal([]byte(`7`), &n)
		return fmt.Sprint(err, int(n))
	})
	check("json-marshal-struct", func() string {
		b, err := json.Marshal(P{"a", 3})
		return fmt.Sprint(string(b), err)
	})
	check("json-marshal-slice", func() string {
		b, err := json.Marshal(L{1, 2})
		return fmt.Sprint(string(b), err)
	})
	check("json-marshal-J", func() string {
		b, err := json.Marshal(J{4})
		return fmt.Sprint(string(b), err)
	})
	check("json-marshal-J-ptr", func() string {
		b, err := json.Marshal(&J{4})
		return fmt.Sprint(string(b), err)
	})
	check("json-unmarshal-U", func() string {
		var u U
		err := json.Unmarshal([]byte(`"abcd"`), &u)
		return fmt.Sprint(err, u.X)
	})
	check("json-unmarshal-in-slice", func() string {
		var ps []P
		err := json.Unmarshal([]byte(`[{"Name":"a"},{"Name":"b"}]`), &ps)
		return fmt.Sprint(err, len(ps), ps)
	})
	check("slices-clone", func() string {
		l := L{1, 2, 3}
		c := slices.Clone(l)
		return fmt.Sprintf("%T %v %v", c, c, []int(c))
	})
	check("slices-sort", func() string {
		l := L{3, 1, 2}
		slices.Sort(l)
		return fmt.Sprint([]int(l))
	})
	check("maps-clone", func() string {
		m := M{"a": 1}
		c := maps.Clone(m)
		return fmt.Sprintf("%T %v", c, c["a"])
	})
	check("deepequal-struct", func() string {
		return fmt.Sprint(reflect.DeepEqual(P{"a", 1}, P{"a", 1}), reflect.DeepEqual(P{"a", 1}, P{"b", 1}))
	})
	check("deepequal-slice", func() string {
		return fmt.Sprint(reflect.DeepEqual(L{1}, L{1}), reflect.DeepEqual(L{1}, []int{1}))
	})
	check("reflect-typeof", func() string {
		return fmt.Sprint(reflect.TypeOf(P{}), reflect.TypeOf(L{}), reflect.TypeOf(S("")), reflect.TypeOf(N(0)))
	})
	check("reflect-valueof-field", func() string {
		v := reflect.ValueOf(P{"z", 9})
		return fmt.Sprint(v.Kind(), v.NumField(), v.Field(0).String())
	})
	check("reflect-valueof-ptr-set", func() string {
		p := P{"z", 9}
		reflect.ValueOf(&p).Elem().Field(1).SetInt(42)
		return fmt.Sprint(p.Age)
	})
	check("errors-is-ptr", func() string {
		e := &E{1}
		w := fmt.Errorf("w: %w", e)
		return fmt.Sprint(errors.Is(w, e), errors.Is(e, e), errors.Is(w, &E{1}))
	})
	check("errors-is-val", func() string {
		e := VE{2}
		w := fmt.Errorf("w: %w", e)
		return fmt.Sprint(errors.Is(w, e), errors.Is(w, VE{3}))
	})
	check("errors-as", func() string {
		w := fmt.Errorf("w: %w", &E{5})
		var t *E
		ok := errors.As(w, &t)
		c := 0
		if ok {
			c = t.Code
		}
		var v VE
		ok2 := errors.As(fmt.Errorf("x: %w", VE{6}), &v)
		return fmt.Sprint(ok, c, ok2, v.Code)
	})
	check("any-eq", func() string {
		e := &E{1}
		var a any = e
		var b any = e
		p := P{"a", 1}
		var c any = p
		return fmt.Sprint(a == b, c == any(P{"a", 1}), any(L(nil)) != nil)
	})
	check("map-key", func() string {
		m := map[any]int{}
		m[P{"a", 1}] = 1
		m[P{"a", 1}]++
		m[S("x")] = 5
		m[N(3)] = 6
		return fmt.Sprint(len(m), m[P{"a", 1}], m[S("x")], m[N(3)])
	})
	check("sort-slice", func() string {
		ps := []P{{"b", 2}, {"a", 1}, {"c", 3}}
		sort.Slice(ps, func(i, j int) bool { return ps[i].Age < ps[j].Age })
		return fmt.Sprint(ps[0].Name, ps[1].Name, ps[2].Name)
	})
	check("sort-slice-named", func() string {
		l := L{3, 1, 2}
		sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
		return fmt.Sprint([]int(l))
	})
	check("fmt-ptr", func() string {
		p := &P{"q", 1}
		return fmt.Sprintf("%v %+v %s", p, p, p)
	})
	check("fmt-vals", func() string {
		return fmt.Sprintf("%v %v %v %v %d", P{"q", 1}, L{1}, S("s"), N(4), N(4))
	})
	check("fmt-T", func() string {
		return fmt.Sprintf("%T %T %T %T %T", P{}, &P{}, L{}, S(""), N(0))
	})
	check("strings-join-sprint", func() string {
		return strings.Join([]string{fmt.Sprint(N(1)), fmt.Sprint(S("a"))}, ",")
	})
	check("any-roundtrip-assert", func() string {
		var x any = P{"a", 1}
		y := reflect.ValueOf(x).Interface()
		p, ok := y.(P)
		return fmt.Sprint(ok, p.Name)
	})
	check("reflect-interface-assert-named", func() string {
		y := reflect.ValueOf(L{1, 2}).Interface()
		l, ok := y.(L)
		return fmt.Sprint(ok, len(l))
	})
	check("deferred-print", func() string {
		var sb strings.Builder
		func() {
			defer fmt.Fprintln(&sb, "deferred", P{"d", 1})
		}()
		return strings.TrimSpace(sb.String())
	})
	check("goroutine-print", func() string {
		var sb strings.Builder
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Fprint(&sb, "go ", P{"g", 1})
		}()
		wg.Wait()
		return sb.String()
	})
	check("logger", func() string {
		var sb strings.Builder
		l := log.New(&sb, "", 0)
		l.Println("logger", P{"l", 1})
		l.Printf("loggerf %v %s %T", P{"l", 1}, P{"l", 1}, P{"l", 1})
		return strings.ReplaceAll(sb.String(), "\n", "|")
	})
	check("encoder", func() string {
		var sb strings.Builder
		enc := json.NewEncoder(&sb)
		e1 := enc.Encode(P{"e", 1})
		e2 := enc.Encode(W{J{1}, P{"w", 2}})
		return fmt.Sprint(e1, e2, " ", strings.ReplaceAll(sb.String(), "\n", "|"))
	})
	check("decoder", func() string {
		var u U
		err := json.NewDecoder(strings.NewReader(`"abc"`)).Decode(&u)
		var p P
		err2 := json.NewDecoder(strings.NewReader(`{"Name":"q","Age":4}`)).Decode(&p)
		return fmt.Sprint(err, u.X, err2, p.Name, p.Age)
	})
	check("go-syntax", func() string {
		return fmt.Sprintf("%v|%+v|%#v|%#v", P{"s", 1}, &P{"s", 1}, []P{{"a", 2}}, map[string]P{"k": {"b", 3}})
	})
	return out.String()
}
