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

package program

// PoolMarks records constant-pool and type-table lengths for restoring a discarded
// compilation or inline splice.
type PoolMarks struct {
	// intConstants is the number of signed integer constants to retain.
	intConstants int

	// floatConstants is the number of floating-point constants to retain.
	floatConstants int

	// stringConstants is the number of string constants to retain.
	stringConstants int

	// boolConstants is the number of boolean constants to retain.
	boolConstants int

	// uintConstants is the number of unsigned integer constants to retain.
	uintConstants int

	// complexConstants is the number of complex constants to retain.
	complexConstants int

	// generalConstants is the number of general-bank constants to retain.
	generalConstants int

	// generalConstantDescriptors is the number of general-constant descriptions to retain.
	generalConstantDescriptors int

	// typeTable is the number of reflected types to retain.
	typeTable int

	// typeTableDescriptors is the number of serialised type descriptions to retain.
	typeTableDescriptors int

	// typeTableInterfaceMethods is the number of interface method-name sets to retain.
	typeTableInterfaceMethods int

	// structLayoutTable is the number of struct layouts to retain.
	structLayoutTable int
}

// CapturePoolMarks records the current length of compiledFunction's pools.
//
// Takes compiledFunction (*CompiledFunction) which is about to have entries appended.
//
// Returns PoolMarks which Restore can truncate the pools back to.
func CapturePoolMarks(compiledFunction *CompiledFunction) PoolMarks {
	return PoolMarks{
		intConstants:               len(compiledFunction.IntConstants),
		floatConstants:             len(compiledFunction.FloatConstants),
		stringConstants:            len(compiledFunction.StringConstants),
		boolConstants:              len(compiledFunction.BoolConstants),
		uintConstants:              len(compiledFunction.UintConstants),
		complexConstants:           len(compiledFunction.ComplexConstants),
		generalConstants:           len(compiledFunction.GeneralConstants),
		generalConstantDescriptors: len(compiledFunction.GeneralConstantDescriptors),
		typeTable:                  len(compiledFunction.TypeTable),
		typeTableDescriptors:       len(compiledFunction.TypeTableDescriptors),
		typeTableInterfaceMethods:  len(compiledFunction.TypeTableInterfaceMethods),
		structLayoutTable:          len(compiledFunction.StructLayoutTable),
	}
}

// Restore truncates compiledFunction's pools to the recorded lengths and prunes the dedup
// entries that point past them.
//
// Takes compiledFunction (*CompiledFunction) which is the function CapturePoolMarks
// recorded; its pools have only grown since.
func (marks *PoolMarks) Restore(compiledFunction *CompiledFunction) {
	compiledFunction.IntConstants = compiledFunction.IntConstants[:marks.intConstants]
	compiledFunction.FloatConstants = compiledFunction.FloatConstants[:marks.floatConstants]
	compiledFunction.StringConstants = compiledFunction.StringConstants[:marks.stringConstants]
	compiledFunction.BoolConstants = compiledFunction.BoolConstants[:marks.boolConstants]
	compiledFunction.UintConstants = compiledFunction.UintConstants[:marks.uintConstants]
	compiledFunction.ComplexConstants = compiledFunction.ComplexConstants[:marks.complexConstants]
	compiledFunction.GeneralConstants = compiledFunction.GeneralConstants[:marks.generalConstants]
	compiledFunction.GeneralConstantDescriptors = compiledFunction.GeneralConstantDescriptors[:marks.generalConstantDescriptors]
	compiledFunction.TypeTable = compiledFunction.TypeTable[:marks.typeTable]
	compiledFunction.TypeTableDescriptors = compiledFunction.TypeTableDescriptors[:marks.typeTableDescriptors]
	compiledFunction.TypeTableInterfaceMethods = compiledFunction.TypeTableInterfaceMethods[:marks.typeTableInterfaceMethods]
	compiledFunction.StructLayoutTable = compiledFunction.StructLayoutTable[:marks.structLayoutTable]

	pruneIndex(compiledFunction.IntConstIndex, marks.intConstants)
	pruneIndex(compiledFunction.FloatConstIndex, marks.floatConstants)
	pruneIndex(compiledFunction.StringConstIndex, marks.stringConstants)
	pruneIndex(compiledFunction.UintConstIndex, marks.uintConstants)
	pruneIndex(compiledFunction.ComplexConstIndex, marks.complexConstants)
	pruneIndex(compiledFunction.TypeRefIndex, marks.typeTable)
	pruneIndex(compiledFunction.typeRefMethodsIndex, marks.typeTable)
}
