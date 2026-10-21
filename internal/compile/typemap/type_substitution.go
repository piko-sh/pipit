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

package typemap

import "go/types"

// SubstituteType lowers t through subs, replacing TypeParams in place.
//
// Takes t (types.Type) which is the type to substitute. Nil returns nil.
// Takes subs (map[*types.TypeParam]types.Type) which maps each generic type parameter to
// its concrete instantiation. May be nil or empty (returns t unchanged).
// Takes cache (map[types.Type]types.Type) which memoises results. Pass nil for one-shot
// calls or a fresh empty map for repeated recursion.
//
// Returns the substituted types.Type, or t when the substitution would be a no-op.
func SubstituteType(t types.Type, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	if t == nil || len(subs) == 0 {
		return t
	}
	if cache != nil {
		if cached, ok := cache[t]; ok {
			return cached
		}
	}
	result := substituteTypeUncached(t, subs, cache)
	if cache != nil {
		cache[t] = result
	}
	return result
}

// substituteTypeUncached is the core walker. Separated from SubstituteType to keep the
// cache-management code out of the hot recursive switch.
//
// Takes t (types.Type) which is the type to substitute (non-nil, subs non-empty per
// SubstituteType's preconditions).
// Takes subs (map[*types.TypeParam]types.Type) which is the substitution map.
// Takes cache (map[types.Type]types.Type) which memoises results.
//
// Returns the substituted types.Type.
func substituteTypeUncached(t types.Type, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	switch u := t.(type) {
	case *types.TypeParam:
		if substituted, ok := subs[u]; ok {
			return substituted
		}
		return t
	case *types.Basic:
		return t
	case *types.Pointer:
		element := SubstituteType(u.Elem(), subs, cache)
		if element == u.Elem() {
			return t
		}
		return types.NewPointer(element)
	case *types.Slice:
		element := SubstituteType(u.Elem(), subs, cache)
		if element == u.Elem() {
			return t
		}
		return types.NewSlice(element)
	case *types.Array:
		return substituteArrayElement(u, subs, cache)
	case *types.Map:
		return substituteMap(u, subs, cache)
	case *types.Chan:
		return substituteChannel(u, subs, cache)
	case *types.Tuple:
		return substituteTuple(u, subs, cache)
	case *types.Signature:
		return substituteSignature(u, subs, cache)
	case *types.Struct:
		return substituteStruct(u, subs, cache)
	case *types.Named:
		return substituteNamed(u, subs, cache)
	case *types.Alias:
		return SubstituteType(types.Unalias(t), subs, cache)
	case *types.Interface:

		return t
	default:
		return t
	}
}

// substituteArrayElement substitutes the element type of a fixed-length array, preserving
// the array length when reconstructing.
//
// Takes u (*types.Array) which is the array type to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted array type, or u when no substitution fires.
func substituteArrayElement(u *types.Array, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	element := SubstituteType(u.Elem(), subs, cache)
	if element == u.Elem() {
		return u
	}
	return types.NewArray(element, u.Len())
}

// substituteMap substitutes both key and element of a map type.
//
// Takes u (*types.Map) which is the map type to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted map type, or u when no substitution fires.
func substituteMap(u *types.Map, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	key := SubstituteType(u.Key(), subs, cache)
	element := SubstituteType(u.Elem(), subs, cache)
	if key == u.Key() && element == u.Elem() {
		return u
	}
	return types.NewMap(key, element)
}

// substituteChannel substitutes the element type of a channel, preserving its direction.
//
// Takes u (*types.Chan) which is the channel type to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted channel type, or u when no substitution fires.
func substituteChannel(u *types.Chan, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	element := SubstituteType(u.Elem(), subs, cache)
	if element == u.Elem() {
		return u
	}
	return types.NewChan(u.Dir(), element)
}

// substituteTuple substitutes each variable in a tuple. Returns the original tuple
// unchanged when no substitution fires.
//
// Takes u (*types.Tuple) which is the tuple to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted tuple type, or u when no substitution fires.
func substituteTuple(u *types.Tuple, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) *types.Tuple {
	if u == nil || u.Len() == 0 {
		return u
	}
	changed := false
	fields := make([]*types.Var, u.Len())
	for i := range u.Len() {
		original := u.At(i)
		subType := SubstituteType(original.Type(), subs, cache)
		if subType == original.Type() {
			fields[i] = original
			continue
		}
		changed = true
		fields[i] = types.NewVar(original.Pos(), original.Pkg(), original.Name(), subType)
	}
	if !changed {
		return u
	}
	return types.NewTuple(fields...)
}

// substituteSignature substitutes parameter, result, and receiver types of a function
// signature. The result has nil type parameters, so callers must read them from the
// original signature.
//
// Takes u (*types.Signature) which is the signature to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted signature, or u when no substitution fires.
func substituteSignature(u *types.Signature, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	parameters := substituteTuple(u.Params(), subs, cache)
	results := substituteTuple(u.Results(), subs, cache)
	receiver := u.Recv()
	if receiver != nil {
		subType := SubstituteType(receiver.Type(), subs, cache)
		if subType != receiver.Type() {
			receiver = types.NewVar(receiver.Pos(), receiver.Pkg(), receiver.Name(), subType)
		}
	}
	if parameters == u.Params() && results == u.Results() && receiver == u.Recv() {
		return u
	}
	return types.NewSignatureType(receiver, nil, nil, parameters, results, u.Variadic())
}

// substituteStruct substitutes each field's type while preserving struct tags and field
// embedding flags.
//
// Takes u (*types.Struct) which is the struct type to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted struct type, or u when no substitution fires.
func substituteStruct(u *types.Struct, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	changed := false
	fields := make([]*types.Var, u.NumFields())
	tags := make([]string, u.NumFields())
	for i := range u.NumFields() {
		original := u.Field(i)
		subType := SubstituteType(original.Type(), subs, cache)
		if subType == original.Type() {
			fields[i] = original
		} else {
			changed = true
			fields[i] = types.NewField(original.Pos(), original.Pkg(), original.Name(), subType, original.Embedded())
		}
		tags[i] = u.Tag(i)
	}
	if !changed {
		return u
	}
	return types.NewStruct(fields, tags)
}

// substituteNamed substitutes the type-args of a named type and re-instantiates against
// its origin. Returns the original named type when it has no type-args or when
// instantiation fails.
//
// Takes u (*types.Named) which is the named type to substitute.
// Takes subs (map[*types.TypeParam]types.Type) which carries the active substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks across shared subtrees.
//
// Returns the substituted named type, or u when no substitution fires.
func substituteNamed(u *types.Named, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) types.Type {
	if u.TypeArgs() == nil || u.TypeArgs().Len() == 0 {
		return u
	}
	changed := false
	args := make([]types.Type, u.TypeArgs().Len())
	for i := range u.TypeArgs().Len() {
		original := u.TypeArgs().At(i)
		args[i] = SubstituteType(original, subs, cache)
		if args[i] != original {
			changed = true
		}
	}
	if !changed {
		return u
	}
	instantiated, err := types.Instantiate(nil, u.Origin(), args, false)
	if err != nil {
		return u
	}
	return instantiated
}
