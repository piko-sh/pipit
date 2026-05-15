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

package engine

import (
	"fmt"
	"reflect"

	"pipit.sh/pipit/internal/fault"
)

// AssignFrameArguments writes host-supplied arguments into the parameter registers of the
// most recently pushed frame.
//
// Every register bank and boxing rule applies unchanged, so a top-level function can be
// called with values the host owns.
//
// Takes arguments ([]reflect.Value) which holds one value per declared parameter.
//
// Returns error wrapping [fault.ErrCallArgumentCount] when the count does not match the
// function's parameter list.
func (vm *VM) AssignFrameArguments(arguments []reflect.Value) error {
	frame := &vm.CallStack[vm.FramePointer]
	function := frame.Function
	if len(arguments) != len(function.ParameterKinds) {
		return fmt.Errorf("%w: %s takes %d argument(s), %d given",
			fault.ErrCallArgumentCount, function.FunctionName(), len(function.ParameterKinds), len(arguments))
	}
	assignReflectParams(&frame.Registers, function.ParameterKinds, function.ParameterRegisters, arguments)
	return nil
}
