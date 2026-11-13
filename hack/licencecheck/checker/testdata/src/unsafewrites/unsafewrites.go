package unsafewrites

import (
	"reflect"
	"sync/atomic"
	"unsafe"
)

type T struct{ p *int }

func allowed(p unsafe.Pointer) *T { return (*T)(p) }

func notAllowed(p unsafe.Pointer) *T { // want `licencecheck unsafe-writes: unsafewrites.notAllowed creates unsafe or reflective write capability \(conv-unsafe->ptr\) and is not in unsafe_allowed`
	return (*T)(p)
}

func reflective(t reflect.Type, p unsafe.Pointer) reflect.Value { // want `licencecheck unsafe-writes: unsafewrites.reflective creates unsafe or reflective write capability \(reflect.NewAt\)`
	return reflect.NewAt(t, p)
}

func slices(p *byte) []byte { // want `licencecheck unsafe-writes: unsafewrites.slices creates unsafe or reflective write capability \(unsafe.Slice\)`
	return unsafe.Slice(p, 4)
}

func readOnly(x int) int { return x + 1 }

func fromAddress(address uintptr) unsafe.Pointer { // want `licencecheck unsafe-writes: unsafewrites.fromAddress creates unsafe or reflective write capability \(conv-uintptr->unsafe\)`
	return unsafe.Pointer(address)
}

func advance(p unsafe.Pointer) unsafe.Pointer { // want `\(unsafe.Add\)`
	return unsafe.Add(p, 8)
}

func publish(slot *unsafe.Pointer, p unsafe.Pointer) { // want `\(atomic.StorePointer\)`
	atomic.StorePointer(slot, p)
}

func swapTyped(slot *atomic.Pointer[T], t *T) *T { // want `\(atomic.Pointer.Swap\)`
	return slot.Swap(t)
}

func storeCounter(counter *atomic.Int64) {
	counter.Store(1)
}

func setThrough(v reflect.Value, p unsafe.Pointer) { // want `\(reflect.Value.SetPointer\)`
	v.SetPointer(p)
}

func memmoveBytes(destination, source []byte) int { return copy(destination, source) }

func moves(destination, source []byte) int { // want `\(memmove:memmoveBytes\)`
	return memmoveBytes(destination, source)
}

func identity[V any](v V) V { return v }

func first[A, B any](a A, _ B) A { return a }

func plainCalls(x int64, t T) (int, T, int) {
	t = T(t)
	length := len([]int{int(x)})
	func() {}()
	return identity[int](length), t, first[int, string](length, "")
}
