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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"piko.sh/safeconv"
)

const (
	// flagMethodBit marks a reflect.Value that is a method value (reflect's flagMethod).
	flagMethodBit uintptr = 1 << 9

	// flagMethodShift is where reflect stores a method value's method index.
	flagMethodShift = 10

	// syncMethodLock names the blocking Lock method of sync.Mutex and sync.RWMutex.
	syncMethodLock = "Lock"

	// syncMethodWait names the blocking Wait method of sync.WaitGroup and sync.Cond.
	syncMethodWait = "Wait"

	// waitGroupCounterShift is the bit position of sync.WaitGroup's int32 counter in its
	// state word; the low half counts waiters.
	waitGroupCounterShift = 32

	// waitGroupLayoutProbe is the count the layout self-test adds to a group before reading
	// the counter back.
	waitGroupLayoutProbe = 3
)

// syncMethodDecoder holds the receiver types' *abi.Type pointers and blocking method
// indices used by decodeSyncWait.
//
// Its ok flag is false if the startup self-test cannot decode every sample method, and
// the decoder then recognises nothing.
var syncMethodDecoder = newSyncMethodDecoder()

// syncMethodDecoderTable is syncMethodDecoder's data.
type syncMethodDecoderTable struct {
	// mutexType, rwMutexType, waitGroupType and condType identify the runtime sync types.
	mutexType, rwMutexType, waitGroupType, condType unsafe.Pointer

	// mutexLock, rwLock, rwRLock, waitGroupWait and condWait identify blocking method
	// indices.
	mutexLock, rwLock, rwRLock, waitGroupWait, condWait int

	// waitGroupStateOffset locates the counter within the runtime WaitGroup representation.
	waitGroupStateOffset uintptr

	// ok and waitGroupOK report whether the method and WaitGroup layouts were recognised.
	ok, waitGroupOK bool
}

// newSyncMethodDecoder builds the decoder and checks it decodes a method value of each
// blocking method to the right kind and receiver.
//
// Returns syncMethodDecoderTable which is the decoder, disabled when the check failed.
func newSyncMethodDecoder() syncMethodDecoderTable {
	index := func(valueType reflect.Type, name string) int {
		method, ok := valueType.MethodByName(name)
		if !ok {
			return -1
		}
		return method.Index
	}
	table := syncMethodDecoderTable{
		mutexType:            reflectValueABIType(reflect.TypeFor[*sync.Mutex]()),
		rwMutexType:          reflectValueABIType(reflect.TypeFor[*sync.RWMutex]()),
		waitGroupType:        reflectValueABIType(reflect.TypeFor[*sync.WaitGroup]()),
		condType:             reflectValueABIType(reflect.TypeFor[*sync.Cond]()),
		mutexLock:            index(reflect.TypeFor[*sync.Mutex](), syncMethodLock),
		rwLock:               index(reflect.TypeFor[*sync.RWMutex](), syncMethodLock),
		rwRLock:              index(reflect.TypeFor[*sync.RWMutex](), "RLock"),
		waitGroupWait:        index(reflect.TypeFor[*sync.WaitGroup](), syncMethodWait),
		condWait:             index(reflect.TypeFor[*sync.Cond](), syncMethodWait),
		waitGroupStateOffset: 0,
		ok:                   false,
		waitGroupOK:          false,
	}
	table.waitGroupStateOffset, table.waitGroupOK = waitGroupStateLayout()
	table.ok = true
	var mutex sync.Mutex
	var rwMutex sync.RWMutex
	var waitGroup sync.WaitGroup
	cond := sync.NewCond(&mutex)
	checks := []struct {
		pointer unsafe.Pointer
		method  reflect.Value
		kind    syncWaitKind
	}{
		{unsafe.Pointer(&mutex), reflect.ValueOf(&mutex).MethodByName(syncMethodLock), syncWaitMutexLock},
		{unsafe.Pointer(&rwMutex), reflect.ValueOf(&rwMutex).MethodByName(syncMethodLock), syncWaitRWMutexLock},
		{unsafe.Pointer(&rwMutex), reflect.ValueOf(&rwMutex).MethodByName("RLock"), syncWaitRWMutexRLock},
		{unsafe.Pointer(&waitGroup), reflect.ValueOf(&waitGroup).MethodByName(syncMethodWait), syncWaitGroupWait},
		{unsafe.Pointer(cond), reflect.ValueOf(cond).MethodByName(syncMethodWait), syncWaitCondWait},
		{nil, reflect.ValueOf(&mutex).MethodByName("Unlock"), syncWaitNone},
		{nil, reflect.ValueOf(&rwMutex).MethodByName("RUnlock"), syncWaitNone},
	}
	for _, check := range checks {
		got := table.decode(check.method)
		if got.kind != check.kind || (check.pointer != nil && got.pointer() != check.pointer) {
			table.ok = false
			break
		}
	}
	return table
}

// waitGroupStateLayout finds sync.WaitGroup's state word and checks that its high 32 bits
// are the counter, by reading a group through Add and Done.
//
// Returns uintptr which is the word's offset.
// Returns bool which is false when the layout is not the expected one.
func waitGroupStateLayout() (uintptr, bool) {
	field, ok := reflect.TypeFor[sync.WaitGroup]().FieldByName("state")
	if !ok || field.Type != reflect.TypeFor[atomic.Uint64]() {
		return 0, false
	}
	var group sync.WaitGroup
	counter := func() uint64 {
		return (*atomic.Uint64)(unsafe.Add(unsafe.Pointer(&group), field.Offset)).Load() >> waitGroupCounterShift
	}
	group.Add(waitGroupLayoutProbe)
	added := counter()
	group.Done()
	afterDone := counter()
	group.Add(1 - waitGroupLayoutProbe)
	return field.Offset, added == waitGroupLayoutProbe && afterDone == waitGroupLayoutProbe-1 && counter() == 0
}

// pointer returns the decoded receiver as an unsafe.Pointer, for the self-test.
//
// Returns unsafe.Pointer which is the receiver, or nil.
func (target syncWaitTarget) pointer() unsafe.Pointer {
	switch target.kind {
	case syncWaitMutexLock:
		return unsafe.Pointer(target.mutex)
	case syncWaitRWMutexLock, syncWaitRWMutexRLock:
		return unsafe.Pointer(target.rwMutex)
	case syncWaitGroupWait:
		return unsafe.Pointer(target.waitGroup)
	case syncWaitCondWait:
		return unsafe.Pointer(target.cond)
	default:
		return nil
	}
}

// decode reads a method value's receiver type, receiver and method index from its
// reflect.Value header and names the blocking sync method it is, if any.
//
// Takes method (reflect.Value) which is the function a call site calls.
//
// Returns syncWaitTarget which is the decoded call, kind syncWaitNone otherwise.
func (table *syncMethodDecoderTable) decode(method reflect.Value) syncWaitTarget {
	if !method.IsValid() || method.Kind() != reflect.Func {
		return syncWaitTarget{}
	}
	header := (*unsafeReflectValue)(unsafe.Pointer(&method))
	if header.flag&flagMethodBit == 0 {
		return syncWaitTarget{}
	}
	methodIndex := int(header.flag >> flagMethodShift)
	receiver := header.ptr
	if header.flag&flagIndir != 0 {
		receiver = *(*unsafe.Pointer)(receiver)
	}
	if receiver == nil {
		return syncWaitTarget{}
	}
	switch header.typ {
	case table.mutexType:
		if methodIndex == table.mutexLock {
			return syncWaitTarget{kind: syncWaitMutexLock, mutex: (*sync.Mutex)(receiver), rwMutex: nil, waitGroup: nil, cond: nil}
		}
	case table.rwMutexType:
		switch methodIndex {
		case table.rwLock:
			return syncWaitTarget{kind: syncWaitRWMutexLock, rwMutex: (*sync.RWMutex)(receiver), mutex: nil, waitGroup: nil, cond: nil}
		case table.rwRLock:
			return syncWaitTarget{kind: syncWaitRWMutexRLock, rwMutex: (*sync.RWMutex)(receiver), mutex: nil, waitGroup: nil, cond: nil}
		}
	case table.waitGroupType:
		if methodIndex == table.waitGroupWait && table.waitGroupOK {
			return syncWaitTarget{kind: syncWaitGroupWait, waitGroup: (*sync.WaitGroup)(receiver), mutex: nil, rwMutex: nil, cond: nil}
		}
	case table.condType:
		if methodIndex == table.condWait {
			return syncWaitTarget{kind: syncWaitCondWait, cond: (*sync.Cond)(receiver), mutex: nil, rwMutex: nil, waitGroup: nil}
		}
	}
	return syncWaitTarget{}
}

// decodeSyncWait names the blocking sync method a call makes, from the method value it
// calls, falling back to the receiver register.
//
// Takes method (reflect.Value) which is the function the call calls.
// Takes receiver (reflect.Value) which is the call's receiver register, or invalid.
//
// Returns syncWaitTarget which is the decoded call, kind syncWaitNone otherwise.
func decodeSyncWait(method, receiver reflect.Value) syncWaitTarget {
	if !syncMethodDecoder.ok {
		return syncWaitFromReceiver(receiver)
	}
	return syncMethodDecoder.decode(method)
}

// waitGroupIdle reports whether a WaitGroup's counter is zero, so its Wait would return.
//
// Takes group (*sync.WaitGroup) which is the group.
//
// Returns bool which is true when the counter is zero or below.
func waitGroupIdle(group *sync.WaitGroup) bool {
	state := (*atomic.Uint64)(unsafe.Add(unsafe.Pointer(group), syncMethodDecoder.waitGroupStateOffset)).Load()

	return safeconv.Uint64ToInt64Reinterpret(state) < 1<<waitGroupCounterShift
}

// syncMethodDecoderAvailable reports whether the method-value decoder passed its start-up
// self-test.
//
// Returns bool which is true when it did.
func syncMethodDecoderAvailable() bool { return syncMethodDecoder.ok && syncMethodDecoder.waitGroupOK }
