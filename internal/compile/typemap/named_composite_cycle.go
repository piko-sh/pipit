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

import (
	"context"
	"fmt"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// cycleIndex finds the groups of self-referential named composite types for one compile.
//
// The parts a group follows (cycleEdges) form a graph over go/types types, and a group is
// the set of named composite types in one strongly connected component of that graph. The
// index computes the components with Tarjan's algorithm as conversion reaches new types
// and remembers each type's component, so a compile's types are each visited once however
// many named types reach them. It also remembers ContainsTypeParameter for each named
// type the graph meets, which would otherwise walk the type again at every edge.
type cycleIndex struct {
	// component maps each type the walk has finished to its component's position in
	// components.
	component map[types.Type]int

	// order maps each type the walk has reached to the order it was reached in.
	order map[types.Type]int

	// low maps each type on the walk's stack to the lowest order reachable from it.
	low map[types.Type]int

	// typeParameters remembers ContainsTypeParameter per named type.
	typeParameters map[*types.Named]bool

	// components lists the finished components.
	components []cycleComponent

	// edgeVisits counts the edges the walk has followed.
	edgeVisits int
}

// cycleComponent is one strongly connected component of the cycle graph.
type cycleComponent struct {
	// members are the named composite types in the component.
	members []*types.Named

	// size is how many types the component holds, members or not.
	size int
}

// tarjanFrame is one type on the walk's explicit call stack.
type tarjanFrame struct {
	// node is the type.
	node types.Type

	// edges are its parts.
	edges []types.Type

	// next is the position of the next part to follow.
	next int
}

// compositeShape is what an unnamed composite type's shape is written from.
type compositeShape struct {
	// parts are its element, its key and value, or its parameters then its results.
	parts []types.Type

	// kind is its kind, or reflect.Invalid for a type that is no composite.
	kind reflect.Kind

	// detail is an array's length or a channel's direction, else zero.
	detail int

	// inCount is a function's parameter count.
	inCount int

	// variadic is true for a variadic function.
	variadic bool
}

// tarjanWalk is the state of one componentOf walk, kept apart from the index so the walk
// can be split into steps.
type tarjanWalk struct {
	// index receives the finished components.
	index *cycleIndex

	// state holds the symbol registry the walk asks about each type.
	state *typeConverterState

	// stack holds the reached types not yet in a finished component.
	stack []types.Type

	// onStack marks the types on stack.
	onStack map[types.Type]bool

	// frames is the walk's explicit call stack.
	frames []tarjanFrame
}

// cycleGroup is one group of named composite types whose underlying types reach one
// another through element, key, pointer, parameter and result types, which the converter
// builds together (typemodel.NamedCompositeQuota.CycleFor).
//
// A struct or an interface ends the reach. A struct field that leads back to a type being
// converted becomes `any` (buildStructFields), so a type reached only through a struct is
// not part of its own underlying runtime type.
type cycleGroup struct {
	// state is the converter state the group is converted with.
	state *typeConverterState

	// members are the group's types.
	members map[*types.Named]bool

	// component is the group's component in state.cycles.
	component int
}

// componentOf returns the component of root, finishing every component the walk from root
// reaches.
//
// A component finished by an earlier call holds no type reached for the first time now,
// since that call followed every part of every type in it, so the walk passes over
// finished types. The walk keeps its own stack, so a long chain of types cannot exhaust
// the goroutine stack, and it charges every type to the converter's step budget.
//
// Takes state (*typeConverterState) which holds the symbol registry and the budget.
// Takes root (types.Type) which is the type whose component is wanted.
//
// Returns int which is the component's position in index.components.
// Returns error which wraps fault.ErrCompileTypeTooComplex past the budget, or the
// context's cause when ctx is done.
func (index *cycleIndex) componentOf(ctx context.Context, state *typeConverterState, root types.Type) (int, error) {
	root = types.Unalias(root)
	if id, done := index.component[root]; done {
		return id, nil
	}
	walk := &tarjanWalk{index: index, state: state, stack: nil, onStack: make(map[types.Type]bool), frames: nil}
	walk.enter(root)
	for len(walk.frames) > 0 {
		if err := state.step(ctx); err != nil {
			return 0, err
		}
		frame := &walk.frames[len(walk.frames)-1]
		if frame.next < len(frame.edges) {
			walk.follow(frame)
			continue
		}
		walk.leave()
	}
	return index.component[root], nil
}

// enter reaches node, numbering it and pushing it and its frame.
//
// Takes node (types.Type) which is the type reached for the first time.
func (walk *tarjanWalk) enter(node types.Type) {
	index := walk.index
	reached := len(index.order)
	index.order[node] = reached
	index.low[node] = reached
	walk.stack = append(walk.stack, node)
	walk.onStack[node] = true
	edges := index.edges(walk.state, node)
	index.edgeVisits += len(edges)
	walk.frames = append(walk.frames, tarjanFrame{node: node, edges: edges, next: 0})
}

// follow takes frame's next part. A part in a finished component is passed over, a part
// reached for the first time is entered, and a part still on the stack lowers the frame's
// low link.
//
// Takes frame (*tarjanFrame) which is the top frame. It is not used after a part is
// entered, which may move the frames.
func (walk *tarjanWalk) follow(frame *tarjanFrame) {
	index := walk.index
	next := types.Unalias(frame.edges[frame.next])
	frame.next++
	if _, done := index.component[next]; done {
		return
	}
	if _, reached := index.order[next]; !reached {
		walk.enter(next)
		return
	}
	if walk.onStack[next] {
		index.low[frame.node] = min(index.low[frame.node], index.order[next])
	}
}

// leave pops the top frame once every part is followed, passes its low link to its
// parent, and finishes its component when the type is the component's root.
func (walk *tarjanWalk) leave() {
	index := walk.index
	node := walk.frames[len(walk.frames)-1].node
	walk.frames = walk.frames[:len(walk.frames)-1]
	if len(walk.frames) > 0 {
		parent := walk.frames[len(walk.frames)-1].node
		index.low[parent] = min(index.low[parent], index.low[node])
	}
	if index.low[node] == index.order[node] {
		walk.finishComponent(node)
	}
}

// finishComponent pops the types down to root off the stack as one component.
//
// Takes root (types.Type) which is the component's first reached type.
func (walk *tarjanWalk) finishComponent(root types.Type) {
	index := walk.index
	id := len(index.components)
	component := cycleComponent{members: nil, size: 0}
	for {
		top := walk.stack[len(walk.stack)-1]
		walk.stack = walk.stack[:len(walk.stack)-1]
		delete(walk.onStack, top)
		delete(index.low, top)
		index.component[top] = id
		component.size++
		if named, ok := top.(*types.Named); ok && index.isCandidate(walk.state, named) {
			component.members = append(component.members, named)
		}
		if top == root {
			break
		}
	}
	index.components = append(index.components, component)
}

// edges returns the parts of t a group follows (cycleEdges), with ContainsTypeParameter
// remembered per named type.
//
// Takes state (*typeConverterState) which holds the symbol registry.
// Takes t (types.Type) which is the type.
//
// Returns []types.Type which are its parts.
func (index *cycleIndex) edges(state *typeConverterState, t types.Type) []types.Type {
	if named, ok := t.(*types.Named); ok && (declaredInHostPackage(state, named) || index.containsTypeParameter(named)) {
		return nil
	}
	return cycleEdges(t)
}

// containsTypeParameter is ContainsTypeParameter remembered per named type.
//
// Takes named (*types.Named) which is the type.
//
// Returns bool which is true when named mentions a type parameter.
func (index *cycleIndex) containsTypeParameter(named *types.Named) bool {
	if known, seen := index.typeParameters[named]; seen {
		return known
	}
	result := ContainsTypeParameter(named)
	index.typeParameters[named] = result
	return result
}

// isCandidate reports whether named can be a member of a group, a named array, slice,
// map, channel or function type with no type parameters that gets a runtime type of its
// own.
//
// Takes state (*typeConverterState) which holds the symbol registry.
// Takes named (*types.Named) which is the type.
//
// Returns bool which is true for such a type.
func (index *cycleIndex) isCandidate(state *typeConverterState, named *types.Named) bool {
	if named.Obj() == nil || declaredInHostPackage(state, named) {
		return false
	}
	switch named.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map, *types.Chan, *types.Signature:
		return !index.containsTypeParameter(named)
	default:
		return false
	}
}

// membersOf returns the group of named composite types whose underlying types reach named
// and that named's underlying type reaches, or nil when named does not reach itself.
//
// Takes state (*typeConverterState) which holds the symbol registry.
// Takes named (*types.Named) which is a named composite type.
//
// Returns []*types.Named which is the group, named first, or nil.
// Returns int which is the group's component.
// Returns error when the walk is cut short.
func (index *cycleIndex) membersOf(ctx context.Context, state *typeConverterState, named *types.Named) ([]*types.Named, int, error) {
	id, err := index.componentOf(ctx, state, named)
	if err != nil {
		return nil, 0, err
	}
	component := index.components[id]
	if component.size < 2 {
		return nil, id, nil
	}
	members := make([]*types.Named, 0, len(component.members))
	members = append(members, named)
	for _, member := range component.members {
		if member != named {
			members = append(members, member)
		}
	}
	return members, id, nil
}

// buildUnderlying converts each member's underlying type, in the order of descriptions,
// with every member resolving to its forged type.
//
// Takes descriptions ([]typemodel.CycleMember) which are the members in typemodel's
// order.
// Takes byName (map[string]*types.Named) which finds each member by qualified name.
// Takes forged (map[string]reflect.Type) which are the forged types by qualified name.
//
// Returns []reflect.Type which are the underlying types in the same order.
// Returns error when a conversion was cut short. The members' types must not be kept.
func (group *cycleGroup) buildUnderlying(ctx context.Context, descriptions []typemodel.CycleMember, byName map[string]*types.Named, forged map[string]reflect.Type) ([]reflect.Type, error) {
	state := group.state
	if state.cycleTypes == nil {
		state.cycleTypes = make(map[*types.Named]reflect.Type)
	}
	for qualifiedName, member := range byName {
		state.cycleTypes[member] = forged[qualifiedName]
	}
	defer func() {
		for _, member := range byName {
			delete(state.cycleTypes, member)
		}
	}()
	underlying := make([]reflect.Type, len(descriptions))
	for index, description := range descriptions {
		member := byName[description.Identity.Qualified()]
		underlying[index] = convertUnderlying(ctx, state, member.Underlying())
	}
	if state.conversionErr != nil {
		return nil, state.conversionErr
	}
	return underlying, nil
}

// fail records a group's failure on the converter state.
//
// Takes err (error) which is the failure.
//
// Returns reflect.Type which is `any`, standing in for the member until the compile
// fails.
func (group *cycleGroup) fail(err error) reflect.Type {
	return group.state.fail(err)
}

// contains reports whether t is in the group's component, which for a part reached from a
// member means it leads back to a member.
//
// Takes t (types.Type) which is a part reached from a member's underlying type.
//
// Returns bool which is true for a part of the cycle.
func (group *cycleGroup) contains(t types.Type) bool {
	id, done := group.state.cycles.component[types.Unalias(t)]
	return done && id == group.component
}

// shape writes t in typemodel's cycle shape notation, with members by name, a part that
// reaches no member by its runtime type, and the composites in between by structure.
//
// The shape describes the type the build produces, which is what a loaded bytecode file
// describes too, so the two find the same group. A named type on the cycle that is not a
// member (a named pointer type) is written through its underlying type and marked as
// being converted while it is, exactly as the converter marks it (convertNamedNonStruct).
// A second meeting on the same path is the type `any` there, so the walk ends, and a part
// that leads back to a member only through such a meeting is written by its runtime type,
// as the build gives it none of the members.
//
// Takes t (types.Type) which is a member's underlying type or a part of it.
// Takes depth (int) which bounds the recursion.
//
// Returns string which is the shape.
// Returns bool which is true when the shape mentions a member.
// Returns error which wraps fault.ErrCompileTypeTooComplex past the depth or step budget.
func (group *cycleGroup) shape(ctx context.Context, t types.Type, depth int) (string, bool, error) {
	t = types.Unalias(t)
	if err := group.state.descend(ctx, depth); err != nil {
		return "", false, err
	}
	if named, ok := t.(*types.Named); ok && group.members[named] {
		identity := NamedCompositeIdentityOf(named)
		return typemodel.CycleShapeMember(identity.PkgPath, identity.Name), true, nil
	}
	if !group.contains(t) {
		return group.leaf(ctx, t), false, nil
	}
	if named, ok := t.(*types.Named); ok {
		if group.state.processing[named] {
			return typemodel.CycleShapeLeaf(reflect.TypeFor[any]()), false, nil
		}
		group.state.processing[named] = true
		shape, reaches, err := group.shape(ctx, named.Underlying(), depth+1)
		delete(group.state.processing, named)
		if err != nil || reaches {
			return shape, reaches, err
		}
		return group.leaf(ctx, t), false, nil
	}
	composite := compositeParts(t)
	shapes := make([]string, len(composite.parts))
	reaches := false
	for index, part := range composite.parts {
		shape, partReaches, err := group.shape(ctx, part, depth+1)
		if err != nil {
			return "", false, err
		}
		shapes[index] = shape
		reaches = reaches || partReaches
	}
	if !reaches {
		return group.leaf(ctx, t), false, nil
	}
	return typemodel.CycleShapeComposite(composite.kind, composite.detail, composite.variadic, shapes, composite.inCount), true, nil
}

// leaf writes a part that reaches no member by the runtime type the converter gives it on
// the current path.
//
// Takes t (types.Type) which is the part.
//
// Returns string which is the part's shape.
func (group *cycleGroup) leaf(ctx context.Context, t types.Type) string {
	return typemodel.CycleShapeLeaf(convertType(ctx, group.state, t))
}

// placeholder returns a member's placeholder for typemodel, an unnamed type of its kind
// and layout that holds no member.
//
// Takes member (*types.Named) which is the member.
// Takes depth (int) which bounds the recursion.
//
// Returns reflect.Type which is the placeholder.
// Returns error which wraps typemodel.ErrNamedCompositeUnsupported, or
// fault.ErrCompileTypeTooComplex past the depth or step budget.
func (group *cycleGroup) placeholder(ctx context.Context, member *types.Named, depth int) (reflect.Type, error) {
	switch underlying := member.Underlying().(type) {
	case *types.Slice:
		return typemodel.CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	case *types.Map:
		return typemodel.CyclePlaceholder(reflect.Map, 0, 0, false, 0, nil)
	case *types.Chan:
		return typemodel.CyclePlaceholder(reflect.Chan, 0, 0, false, 0, nil)
	case *types.Signature:
		return typemodel.CyclePlaceholder(reflect.Func, underlying.Params().Len(), underlying.Results().Len(), underlying.Variadic(), 0, nil)
	case *types.Array:
		element, err := group.layoutType(ctx, underlying.Elem(), depth+1)
		if err != nil {
			return nil, err
		}
		return typemodel.CyclePlaceholder(reflect.Array, 0, 0, false, int(underlying.Len()), element)
	default:
		return nil, fmt.Errorf("%w: %s", typemodel.ErrNamedCompositeUnsupported, member)
	}
}

// layoutType returns a type with the layout t will have once the group is built, for an
// array member's placeholder.
//
// Takes t (types.Type) which is an array member's element type or a part of it.
// Takes depth (int) which bounds the recursion.
//
// Returns reflect.Type which has t's layout.
// Returns error which wraps typemodel.ErrNamedCompositeUnsupported, or
// fault.ErrCompileTypeTooComplex past the depth or step budget.
func (group *cycleGroup) layoutType(ctx context.Context, t types.Type, depth int) (reflect.Type, error) {
	t = types.Unalias(t)
	if err := group.state.descend(ctx, depth); err != nil {
		return nil, err
	}
	if named, ok := t.(*types.Named); ok && group.members[named] {
		return group.placeholder(ctx, named, depth+1)
	}
	if !group.contains(t) {
		return convertType(ctx, group.state, t), nil
	}
	switch typed := t.(type) {
	case *types.Named:
		return group.layoutType(ctx, typed.Underlying(), depth+1)
	case *types.Array:
		element, err := group.layoutType(ctx, typed.Elem(), depth+1)
		if err != nil {
			return nil, err
		}
		return typemodel.ArrayOfChecked(int(typed.Len()), element)
	case *types.Slice:
		return typemodel.CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	case *types.Map:
		return typemodel.CyclePlaceholder(reflect.Map, 0, 0, false, 0, nil)
	case *types.Chan:
		return typemodel.CyclePlaceholder(reflect.Chan, 0, 0, false, 0, nil)
	case *types.Pointer:
		return reflect.TypeFor[*byte](), nil
	case *types.Signature:
		return typemodel.CyclePlaceholder(reflect.Func, typed.Params().Len(), typed.Results().Len(), typed.Variadic(), 0, nil)
	default:
		return nil, fmt.Errorf("%w: %s", typemodel.ErrNamedCompositeUnsupported, t)
	}
}

// newCycleIndex returns an index that has seen no types yet.
//
// Returns *cycleIndex which is empty.
func newCycleIndex() *cycleIndex {
	return &cycleIndex{
		component:      make(map[types.Type]int),
		components:     nil,
		order:          make(map[types.Type]int),
		low:            make(map[types.Type]int),
		typeParameters: make(map[*types.Named]bool),
		edgeVisits:     0,
	}
}

// convertNamedCompositeCycle gives every member of the group named belongs to its runtime
// type, and returns named's.
//
// Takes state (*typeConverterState) which carries the cache and the recursion path.
// Takes named (*types.Named) which is the member being converted.
// Takes members ([]*types.Named) which is its group, named included.
// Takes component (int) which is the group's component in state.cycles.
//
// Returns reflect.Type which is named's runtime type, or `any` when the group cannot be
// built (the failure is recorded on state so the compile fails).
func convertNamedCompositeCycle(ctx context.Context, state *typeConverterState, named *types.Named, members []*types.Named, component int) reflect.Type {
	group := &cycleGroup{state: state, members: make(map[*types.Named]bool, len(members)), component: component}
	for _, member := range members {
		group.members[member] = true
		state.processing[member] = true
	}
	defer func() {
		for _, member := range members {
			delete(state.processing, member)
		}
	}()
	descriptions := make([]typemodel.CycleMember, 0, len(members))
	byName := make(map[string]*types.Named, len(members))
	for _, member := range members {
		placeholder, err := group.placeholder(ctx, member, 0)
		if err != nil {
			return group.fail(err)
		}
		shape, _, err := group.shape(ctx, member.Underlying(), 0)
		if err != nil {
			return group.fail(err)
		}
		identity := NamedCompositeIdentityOf(member)
		descriptions = append(descriptions, typemodel.CycleMember{Placeholder: placeholder, Identity: identity, Shape: shape})
		byName[identity.Qualified()] = member
	}
	if state.conversionErr != nil {
		return group.fail(state.conversionErr)
	}
	built, err := state.quota().CycleFor(descriptions, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return group.buildUnderlying(ctx, descriptions, byName, forged)
	})
	if err != nil {
		return group.fail(err)
	}
	for qualifiedName, member := range byName {
		storeReflectType(state, member, built[qualifiedName])
	}
	return built[NamedCompositeIdentityOf(named).Qualified()]
}

// cycleEdges returns the parts of t a group follows.
//
// Takes t (types.Type) which is the type.
//
// Returns []types.Type which are its parts, or nil for a type that ends the reach.
func cycleEdges(t types.Type) []types.Type {
	switch typed := types.Unalias(t).(type) {
	case *types.Named:
		switch typed.Underlying().(type) {
		case *types.Pointer, *types.Slice, *types.Array, *types.Map, *types.Chan, *types.Signature:
			return []types.Type{typed.Underlying()}
		default:
			return nil
		}
	case *types.Slice:
		return []types.Type{typed.Elem()}
	case *types.Array:
		return []types.Type{typed.Elem()}
	case *types.Pointer:
		return []types.Type{typed.Elem()}
	case *types.Chan:
		return []types.Type{typed.Elem()}
	case *types.Map:
		return []types.Type{typed.Key(), typed.Elem()}
	case *types.Signature:
		parts := make([]types.Type, 0, typed.Params().Len()+typed.Results().Len())
		for variable := range typed.Params().Variables() {
			parts = append(parts, variable.Type())
		}
		for variable := range typed.Results().Variables() {
			parts = append(parts, variable.Type())
		}
		return parts
	default:
		return nil
	}
}

// compositeParts splits an unnamed composite type into what its shape is written from.
//
// Takes t (types.Type) which is a slice, array, pointer, channel, map or function type.
//
// Returns compositeShape whose kind is reflect.Invalid for any other type.
func compositeParts(t types.Type) compositeShape {
	switch typed := t.(type) {
	case *types.Slice:
		return compositeShape{kind: reflect.Slice, detail: 0, variadic: false, inCount: 0, parts: []types.Type{typed.Elem()}}
	case *types.Array:
		return compositeShape{kind: reflect.Array, detail: int(typed.Len()), variadic: false, inCount: 0, parts: []types.Type{typed.Elem()}}
	case *types.Pointer:
		return compositeShape{kind: reflect.Pointer, detail: 0, variadic: false, inCount: 0, parts: []types.Type{typed.Elem()}}
	case *types.Chan:
		return compositeShape{kind: reflect.Chan, detail: int(channelDirection(typed.Dir())), variadic: false, inCount: 0, parts: []types.Type{typed.Elem()}}
	case *types.Map:
		return compositeShape{kind: reflect.Map, detail: 0, variadic: false, inCount: 0, parts: []types.Type{typed.Key(), typed.Elem()}}
	case *types.Signature:
		return compositeShape{kind: reflect.Func, detail: 0, variadic: typed.Variadic(), inCount: typed.Params().Len(), parts: cycleEdges(typed)}
	default:
		return compositeShape{kind: reflect.Invalid, detail: 0, variadic: false, inCount: 0, parts: nil}
	}
}

// channelDirection converts a go/types channel direction to reflect's.
//
// Takes direction (types.ChanDir) which is the direction.
//
// Returns reflect.ChanDir which is the same direction.
func channelDirection(direction types.ChanDir) reflect.ChanDir {
	switch direction {
	case types.SendOnly:
		return reflect.SendDir
	case types.RecvOnly:
		return reflect.RecvDir
	default:
		return reflect.BothDir
	}
}

// tooComplex wraps fault.ErrCompileTypeTooComplex.
//
// Takes detail (string) which says which bound was passed.
//
// Returns error which wraps the sentinel.
func tooComplex(detail string) error {
	return fmt.Errorf("%w: %s", fault.ErrCompileTypeTooComplex, detail)
}
