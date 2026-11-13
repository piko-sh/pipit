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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type storeBarrierCase struct {
	source    string
	want      string
	wantLater string
}

func runStoreBarrierCase(t *testing.T, testCase storeBarrierCase) {
	t.Helper()
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, testCase.source)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: package initialisers")
	observations := &hazardObservations{}
	hazardCall(t, service, compiled, "Store", 41)
	first, ok := hazardCall(t, service, compiled, "Read").(string)
	require.True(t, ok, "test harness: Read did not return a string")
	observations.compareString("read straight after the store", testCase.want, first)
	clobberArenas(t, service, 8)
	for range 4 {
		hazardCall(t, service, compiled, "Store", 9000)
	}
	again, ok := hazardCall(t, service, compiled, "Read").(string)
	require.True(t, ok, "test harness: Read did not return a string")
	observations.compareString("global read by a later run after more stores", testCase.wantLater, again)
	hazardCall(t, service, compiled, "Store", 41)
	clobberArenas(t, service, 8)
	last, ok := hazardCall(t, service, compiled, "Read").(string)
	require.True(t, ok, "test harness: Read did not return a string")
	observations.compareString("global read after the store and 8 clobber runs", testCase.want, last)
	window.settle(observations)
	runtime.KeepAlive(first)
	assertNoHazard(t, observations, "")
}

func TestPointerFieldStoreOfASliceHeaderSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "41/82", wantLater: "9000/18000", source: `package main

import "strconv"

type H struct {
	P *[]int
	W int
}

var g = &H{}

func Store(n int) int {
	s := []int{n, n * 2}
	g.P = &s
	return len(s)
}

func Read() string {
	if g.P == nil {
		return "<nil>"
	}
	s := *g.P
	if len(s) < 2 {
		return "len=" + strconv.Itoa(len(s))
	}
	return strconv.Itoa(s[0]) + "/" + strconv.Itoa(s[1])
}
`})
}

func TestInterfaceFieldStoreOfASliceHeaderSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "41/82", wantLater: "9000/18000", source: `package main

import "strconv"

type H struct {
	V any
	W int
}

var g = &H{}

func Store(n int) int {
	s := []int{n, n * 2}
	g.V = &s
	return len(s)
}

func Read() string {
	p, ok := g.V.(*[]int)
	if !ok || p == nil {
		return "<nil>"
	}
	s := *p
	if len(s) < 2 {
		return "len=" + strconv.Itoa(len(s))
	}
	return strconv.Itoa(s[0]) + "/" + strconv.Itoa(s[1])
}
`})
}

func TestStarAppendByteIntoAGlobalSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "pqrstuvwxyzabcdefghijklm", wantLater: "efghijklmnopqrstuvwxyzab", source: `package main

var buf []byte

func Store(n int) int {
	buf = nil
	p := &buf
	for i := 0; i < 24; i++ {
		*p = append(*p, byte('a'+(n+i)%26))
	}
	return len(buf)
}

func Read() string {
	return string(buf)
}
`})
}

func TestStarAppendSpreadIntoAGlobalSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "pqrstuvwxyzabcdefghijklm", wantLater: "efghijklmnopqrstuvwxyzab", source: `package main

var buf []byte

func Store(n int) int {
	buf = nil
	p := &buf
	src := make([]byte, 24)
	for i := range src {
		src[i] = byte('a' + (n+i)%26)
	}
	*p = append(*p, src...)
	return len(buf)
}

func Read() string {
	return string(buf)
}
`})
}

func TestTypedSliceFieldStoreSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "41/82", wantLater: "9000/18000", source: `package main

import "strconv"

type H struct {
	S []int
	W int
}

var g = &H{}

func mk(n int) []int {
	s := make([]int, 2, 8)
	s[0] = n
	s[1] = n * 2
	return s
}

func Store(n int) int {
	for i := 0; i < 3; i++ {
		g.S = mk(n)
	}
	return len(g.S)
}

func Read() string {
	s := g.S
	if len(s) < 2 {
		return "len=" + strconv.Itoa(len(s))
	}
	return strconv.Itoa(s[0]) + "/" + strconv.Itoa(s[1])
}
`})
}

func TestStarAppendThroughAHostPointerSurvivesReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

func Fill(p *[]byte, n int) int {
	for i := 0; i < 24; i++ {
		*p = append(*p, byte('a'+(n+i)%26))
	}
	return len(*p)
}
`)
	observations := &hazardObservations{}
	var host []byte
	hazardCall(t, service, compiled, "Fill", &host, 41)
	const want = "pqrstuvwxyzabcdefghijklm"
	observations.compareString("host slice straight after the run", want, string(host))
	clobberArenas(t, service, 8)
	var other []byte
	for range 8 {
		other = nil
		hazardCall(t, service, compiled, "Fill", &other, 9000)
	}
	observations.compareString("host slice after 8 clobber runs and 8 fills", want, string(host))
	window.settle(observations)
	runtime.KeepAlive(host)
	runtime.KeepAlive(other)
	assertNoHazard(t, observations, "")
}

func TestSeededStructLiteralFieldStoreSurvivesReuse(t *testing.T) {
	runStoreBarrierCase(t, storeBarrierCase{want: "41/82", wantLater: "9000/18000", source: `package main

import "strconv"

type Node struct {
	P    *[]int
	Next *Node
}

var g *Node

func Store(n int) int {
	s := []int{n, n * 2}
	node := &Node{}
	node.P = &s
	g = node
	return len(s)
}

func Read() string {
	if g == nil || g.P == nil {
		return "<nil>"
	}
	s := *g.P
	if len(s) < 2 {
		return "len=" + strconv.Itoa(len(s))
	}
	return strconv.Itoa(s[0]) + "/" + strconv.Itoa(s[1])
}
`})
}

func TestStoreIntoARevokedBackingSurvivesReuse(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	var names []string
	names = append(names, "", "", "", "")
	p := &names
	retainhost.Retain(p)
	names[1] = "revoked-" + strconv.Itoa(n) + "-stored-into-the-revoked-backing"
	retainhost.Settle()
	return len(names[1])
}
`})
}

func TestHostReadOfAStoreLaterOverwrittenSurvivesReuse(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	var names []string
	names = append(names, "", "", "", "")
	p := &names
	retainhost.Retain(p)
	names[1] = "overwritten-" + strconv.Itoa(n) + "-read-by-the-host-before-the-overwrite"
	retainhost.Snapshot()
	names[1] = "constant"
	retainhost.Settle()
	return len(names[1])
}
`})
}
