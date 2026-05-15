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

package pipit

import (
	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/policy"
)

const (
	// SessionDeclVar is a var declaration.
	SessionDeclVar = app.SessionDeclVar

	// SessionDeclFunc is a func declaration.
	SessionDeclFunc = app.SessionDeclFunc

	// SessionDeclType is a type declaration.
	SessionDeclType = app.SessionDeclType

	// SessionDeclConst is a const declaration.
	SessionDeclConst = app.SessionDeclConst

	// bytecodeStoreDirPerm is the permission bits for a bytecode store directory pipit
	// creates.
	bytecodeStoreDirPerm = 0o755

	// bytecodeStoreFilePerm is the permission bits for a bytecode artefact pipit stages.
	bytecodeStoreFilePerm = 0o600

	// bytecodeFilePrefix brackets the leading portion of the on-disk name the store derives
	// from a key, so a caller-chosen path can be translated to and from it.
	bytecodeFilePrefix = "bytecode-"

	// bytecodeFileSuffix brackets the trailing portion of the on-disk name the store derives
	// from a key.
	bytecodeFileSuffix = ".bin"
)

// WallClock is the default Clock; every method delegates to the stdlib time package.
var WallClock = clock.WallClock

// SymbolExports maps an import path to that package's exported symbols by name. It must
// remain a type alias because generated symbol files assign a bare map directly.
type SymbolExports = symtab.SymbolExports

// SymbolProviderPort supplies pre-registered host symbols to an interpreter.
type SymbolProviderPort = symtab.SymbolProviderPort

// SymbolRegistry holds the resolved host symbols an interpreter can call.
type SymbolRegistry = symtab.SymbolRegistry

// Option configures an Interpreter at construction.
type Option = app.Option

// CompiledFileSet is a compiled program: one or more packages plus their entrypoints.
type CompiledFileSet = program.CompiledFileSet

// CompiledFunction is a single compiled function within a CompiledFileSet.
type CompiledFunction = program.CompiledFunction

// BytecodeStorePort persists and reloads compiled programs.
type BytecodeStorePort = program.BytecodeStorePort

// DenyCapabilityHook denies every typed capability (files, network, subprocess and
// environment) with ErrCapabilityDenied while permitting the catch-all CheckFunctionCall,
// so a tier that installs it runs pure computation but refuses every host-touching
// capability. Embed it to permit only the capabilities a host allows.
type DenyCapabilityHook = policy.DenyCapabilityHook

// PermissiveCapabilityHook allows every capability. Embed it to build a hook that
// overrides only the gates a host wants to enforce.
type PermissiveCapabilityHook = policy.PermissiveCapabilityHook

// CapabilityHook is consulted before a gated native operation. A nil hook permits
// everything, which is the wrong default for untrusted code.
type CapabilityHook = policy.CapabilityHook

// Clock is the time source interpreted code observes. Install one for deterministic
// replay or test-controllable time.
type Clock = clock.Clock

// SessionOption configures a Session at construction.
type SessionOption = app.SessionOption

// SessionState is a Session's accumulated declarations.
type SessionState = app.SessionState

// FastPathStats holds the interpreter's accumulated fast-path counters.
type FastPathStats = app.FastPathStats

// SessionDecl is one declaration a Session has accumulated.
type SessionDecl = app.SessionDecl

// SessionDeclKind classifies a SessionDecl.
type SessionDeclKind = app.SessionDeclKind
