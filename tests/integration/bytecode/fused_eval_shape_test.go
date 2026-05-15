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

//go:build integration

package bytecode_test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	fusedEvalTestSource = `package main

const resultMask = 0xFFFFFFFF

type Node interface {
	Eval(env []uint32) uint32
}

type addNode struct{ left, right Node }

func (node *addNode) Eval(env []uint32) uint32 {
	return (node.left.Eval(env) + node.right.Eval(env)) & resultMask
}

type subNode struct{ left, right Node }

func (node *subNode) Eval(env []uint32) uint32 {
	return (node.left.Eval(env) - node.right.Eval(env)) & resultMask
}

type mulNode struct{ left, right Node }

func (node *mulNode) Eval(env []uint32) uint32 {
	return (node.left.Eval(env) * node.right.Eval(env)) & resultMask
}

type modNode struct{ left, right Node }

func (node *modNode) Eval(env []uint32) uint32 {
	rightValue := node.right.Eval(env)
	if rightValue == 0 {
		return 0
	}
	return node.left.Eval(env) % rightValue
}

type minNode struct{ left, right Node }

func (node *minNode) Eval(env []uint32) uint32 {
	leftValue := node.left.Eval(env)
	rightValue := node.right.Eval(env)
	if leftValue < rightValue {
		return leftValue
	}
	return rightValue
}

type maxNode struct{ left, right Node }

func (node *maxNode) Eval(env []uint32) uint32 {
	leftValue := node.left.Eval(env)
	rightValue := node.right.Eval(env)
	if leftValue > rightValue {
		return leftValue
	}
	return rightValue
}

type ifPosNode struct{ condition, thenBranch, elseBranch Node }

func (node *ifPosNode) Eval(env []uint32) uint32 {
	if node.condition.Eval(env) != 0 {
		return node.thenBranch.Eval(env)
	}
	return node.elseBranch.Eval(env)
}

type constNode struct{ value uint32 }

func (node *constNode) Eval(env []uint32) uint32 { return node.value }

type varNode struct{ slot int }

func (node *varNode) Eval(env []uint32) uint32 { return env[node.slot] }

func buildTree(depth int) Node {
	if depth == 0 {
		return &varNode{slot: depth % 4}
	}
	left := buildTree(depth - 1)
	right := &constNode{value: uint32(depth*7 + 1)}
	switch depth % 7 {
	case 0:
		return &addNode{left: left, right: right}
	case 1:
		return &subNode{left: left, right: right}
	case 2:
		return &mulNode{left: left, right: right}
	case 3:
		return &modNode{left: left, right: right}
	case 4:
		return &minNode{left: left, right: right}
	case 5:
		return &maxNode{left: left, right: right}
	default:
		return &ifPosNode{condition: left, thenBranch: right, elseBranch: &constNode{value: 3}}
	}
}

func EntrypointRun() uint32 {
	env := []uint32{11, 22, 33, 44}
	root := buildTree(12)
	total := uint32(0)
	for i := 0; i < 50; i++ {
		total = (total + root.Eval(env)) & resultMask
		env[0] = total
	}
	return total
}`
)

func TestFusedEvalShapeClassification(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": fusedEvalTestSource})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	t.Logf("fused eval result: %v", result)
	shapes := map[string]program.FusedEvalShape{}
	for _, fn := range program.ExportFunctions(compiled.Root()) {
		if fn != nil {
			shapes[fn.Name] = fn.EvalShape
		}
	}
	expected := map[string]program.FusedEvalShape{
		"addNode.Eval":   program.FusedEvalAddMasked,
		"subNode.Eval":   program.FusedEvalSubMasked,
		"mulNode.Eval":   program.FusedEvalMulMasked,
		"modNode.Eval":   program.FusedEvalModGuarded,
		"minNode.Eval":   program.FusedEvalMin,
		"maxNode.Eval":   program.FusedEvalMax,
		"ifPosNode.Eval": program.FusedEvalIfPos,
	}
	for name, want := range expected {
		got, found := findShapeByName(shapes, name)
		if !found {
			t.Errorf("function %q not found in compile output; names: %v", name, slices.Sorted(maps.Keys(shapes)))
			continue
		}
		if got != want {
			t.Errorf("function %q evalShape = %d, want %d", name, got, want)
		}
	}
}

func findShapeByName(shapes map[string]program.FusedEvalShape, suffix string) (program.FusedEvalShape, bool) {
	for name, shape := range shapes {
		if strings.HasSuffix(name, suffix) {
			return shape, true
		}
	}
	return program.FusedEvalNone, false
}
