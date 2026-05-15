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

package compile

import (
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/compile/liveness"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

// activeDeclaration tracks a variable binding introduced by a declaring statement whose
// register must survive across subsequent statements.
type activeDeclaration struct {
	// Name is the variable name introduced by the declaration.
	Name string

	// location is the register allocation for the variable.
	location program.VarLocation
}

// recycleOrTrackDeclarations appends each name introduced by a declaring statement to
// active, or restores the register-allocation watermark when statement does not declare
// any name.
//
// Takes statement (ast.Stmt) which is the statement being processed for declarations.
// Takes watermark ([NumRegisterKinds]uint32) which is the register-allocation watermark
// to restore when no declaration is present.
// Takes active ([]activeDeclaration) which is the current list of tracked active
// declarations.
//
// Returns the updated list of active declarations.
func (c *Compiler) recycleOrTrackDeclarations(
	statement ast.Stmt,
	watermark [isa.NumRegisterKinds]uint32,
	active []activeDeclaration,
) []activeDeclaration {
	if !isDeclaringStatement(statement) {
		c.Scopes.RestoreWatermark(watermark)
		return active
	}
	for _, name := range liveness.ExtractDeclaredNames(statement) {
		active = c.trackDeclaredName(name, active)
	}
	return active
}

// trackDeclaredName appends a new activeDeclaration for name when it is visible in the
// current scope and not already tracked.
//
// Takes name (string) which is the declared variable name to track.
// Takes active ([]activeDeclaration) which is the current list of tracked declarations.
//
// Returns the updated list of active declarations.
func (c *Compiler) trackDeclaredName(name string, active []activeDeclaration) []activeDeclaration {
	declarationLocation, ok := c.Scopes.LookupVar(name)
	if !ok {
		return active
	}
	for _, existing := range active {
		if existing.Name == name {
			return active
		}
	}
	return append(active, activeDeclaration{Name: name, location: declarationLocation})
}

// recycleDeadDeclarations drops declarations whose last-use statement has been compiled
// and recycles their registers, returning a filtered list (or active unchanged when
// lastUseIndices is nil).
//
// Takes active ([]activeDeclaration) which is the current list of tracked declarations.
// Takes lastUseIndices (map[string]int) which maps each name to the index of its last
// use.
// Takes currentIndex (int) which is the index of the statement just compiled.
//
// Returns the filtered list of declarations whose registers must survive.
func (c *Compiler) recycleDeadDeclarations(
	active []activeDeclaration,
	lastUseIndices map[string]int,
	currentIndex int,
) []activeDeclaration {
	if lastUseIndices == nil {
		return active
	}
	remaining := active[:0]
	for _, declaration := range active {
		if c.shouldRetainDeclaration(declaration, lastUseIndices, currentIndex) {
			remaining = append(remaining, declaration)
		}
	}
	return remaining
}

// shouldRetainDeclaration reports whether declaration's register must survive past
// currentIndex. Upvalue, captured, and indirect locations are always retained; otherwise
// the decision follows the recorded last-use index.
//
// Takes declaration (activeDeclaration) which is the declaration whose retention is being
// decided.
// Takes lastUseIndices (map[string]int) which maps each name to the index of its last
// use.
// Takes currentIndex (int) which is the index of the statement just compiled.
//
// Returns true when the declaration's register must survive past currentIndex.
func (c *Compiler) shouldRetainDeclaration(
	declaration activeDeclaration,
	lastUseIndices map[string]int,
	currentIndex int,
) bool {
	lastUse, tracked := lastUseIndices[declaration.Name]
	if !tracked || lastUse > currentIndex {
		return true
	}
	currentLocation, found := c.Scopes.LookupVar(declaration.Name)
	if !found || currentLocation.IsUpvalue || currentLocation.IsCaptured || currentLocation.IsIndirect {
		return true
	}
	c.Scopes.Alloc.RecycleRegister(currentLocation.Kind, currentLocation.Register)
	return false
}

// isDeclaringStatement reports whether statement introduces new variable bindings whose
// registers must survive across subsequent statements. Short variable declarations (:=),
// var/const/type declarations, and labelled wrappers around them all qualify.
//
// Takes statement (ast.Stmt) which is the candidate AST statement.
//
// Returns true when the statement introduces a new variable binding.
func isDeclaringStatement(statement ast.Stmt) bool {
	switch s := statement.(type) {
	case *ast.AssignStmt:
		return s.Tok == token.DEFINE
	case *ast.DeclStmt:
		return true
	case *ast.LabeledStmt:
		return isDeclaringStatement(s.Stmt)
	default:
		return false
	}
}
