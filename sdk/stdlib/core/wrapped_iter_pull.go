// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package core

import (
	"reflect"
)

// iterPackagePath is the import path the Pull wrappers register under.
const iterPackagePath = "iter"

// boolReflectType is the type of the ok result of a pulled next function.
var boolReflectType = reflect.TypeFor[bool]()

// drainValues receives from ch until it is closed, releasing a producer that is parked on
// a send after stop was called.
//
// Takes ch (<-chan T) which is the channel to drain.
func drainValues[T any](ch <-chan T) {
	for {
		if _, ok := <-ch; !ok {
			return
		}
	}
}

// wrappedIterPull stands in for iter.Pull, running the sequence on its own goroutine.
//
// Takes sequence (any) which is an iter.Seq[V] value.
//
// Returns next (any) which is a `func() (V, bool)`.
// Returns stop (any) which is a `func()`.
//
// Safe for concurrent use; the producer goroutine is parked until next or stop is called.
func wrappedIterPull(sequence any) (next any, stop any) {
	sequenceVal := reflect.ValueOf(sequence)
	valueType := pullYieldParameter(sequenceVal.Type(), 0)
	values := make(chan reflect.Value)
	done := make(chan struct{})
	stopped := false
	stopFunction := func() {
		if stopped {
			return
		}
		stopped = true
		close(done)
		drainValues(values)
	}
	go func() {
		defer close(values)
		for v := range sequenceVal.Seq() {
			select {
			case values <- v:
			case <-done:
				return
			}
		}
	}()
	nextType := reflect.FuncOf(nil, []reflect.Type{valueType, boolReflectType}, false)
	nextFunction := reflect.MakeFunc(nextType, func([]reflect.Value) []reflect.Value {
		v, ok := <-values
		if !ok {
			return []reflect.Value{reflect.Zero(valueType), reflect.ValueOf(false)}
		}
		return []reflect.Value{v.Convert(valueType), reflect.ValueOf(true)}
	})
	return nextFunction.Interface(), stopFunction
}

// wrappedIterPull2 stands in for iter.Pull2, running the sequence on its own goroutine.
//
// Takes sequence (any) which is an iter.Seq2[K, V] value.
//
// Returns next (any) which is a `func() (K, V, bool)`.
// Returns stop (any) which is a `func()`.
//
// Safe for concurrent use; the producer goroutine is parked until next or stop is called.
func wrappedIterPull2(sequence any) (next any, stop any) {
	sequenceVal := reflect.ValueOf(sequence)
	keyType := pullYieldParameter(sequenceVal.Type(), 0)
	valueType := pullYieldParameter(sequenceVal.Type(), 1)
	type pair struct {
		k reflect.Value
		v reflect.Value
	}
	values := make(chan pair)
	done := make(chan struct{})
	stopped := false
	stopFunction := func() {
		if stopped {
			return
		}
		stopped = true
		close(done)
		drainValues(values)
	}
	go func() {
		defer close(values)
		for k, v := range sequenceVal.Seq2() {
			select {
			case values <- pair{k: k, v: v}:
			case <-done:
				return
			}
		}
	}()
	nextType := reflect.FuncOf(nil, []reflect.Type{keyType, valueType, boolReflectType}, false)
	nextFunction := reflect.MakeFunc(nextType, func([]reflect.Value) []reflect.Value {
		p, ok := <-values
		if !ok {
			return []reflect.Value{reflect.Zero(keyType), reflect.Zero(valueType), reflect.ValueOf(false)}
		}
		return []reflect.Value{p.k.Convert(keyType), p.v.Convert(valueType), reflect.ValueOf(true)}
	})
	return nextFunction.Interface(), stopFunction
}

// pullYieldParameter returns the type of the index-th parameter of a sequence's yield
// function (`func(yield func(K, V) bool)`), or `any` when the sequence is not such a
// function.
//
// Takes sequenceType (reflect.Type) which is the sequence's type.
// Takes index (int) which is the yield parameter position.
//
// Returns reflect.Type which is the parameter type.
func pullYieldParameter(sequenceType reflect.Type, index int) reflect.Type {
	anyType := reflect.TypeFor[any]()
	if sequenceType == nil || sequenceType.Kind() != reflect.Func || sequenceType.NumIn() != 1 {
		return anyType
	}
	yield := sequenceType.In(0)
	if yield.Kind() != reflect.Func || index >= yield.NumIn() {
		return anyType
	}
	return yield.In(index)
}

func init() {
	if _, ok := Symbols[iterPackagePath]; !ok {
		Symbols[iterPackagePath] = make(map[string]reflect.Value, 2)
	}
	Symbols[iterPackagePath]["Pull"] = reflect.ValueOf(wrappedIterPull)
	Symbols[iterPackagePath]["Pull2"] = reflect.ValueOf(wrappedIterPull2)
}
