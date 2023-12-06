//go:build gc.extalloc_leaking

package runtime

// This is a leaking GC (not a real one) implementation for WebAssembly
// (polkawasm) target that only allocates memory through the external
// allocator but never frees it. However, it is useful for testing purposes
// and performance comparisons.

import (
	"unsafe"
)

// Total number of calls to free()
const gcFrees = 0

var (
	// Total number of calls to alloc()
	gcMallocs uint64

	// Total amount of memory allocated on the heap
	gcTotalAlloc uint64

	// Returned for zero-sized allocations to avoid returning nil pointers.
	zeroSizedAlloc uint8
)

// The heap is initialized by the external allocator.
func initHeap() {}

func setHeapEnd(newHeapEnd uintptr) {}

// Tries to find free space on the heap to allocate memory of the
// given size and returns a pointer to it. If no space is free, it panics.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	size += align(unsafe.Sizeof(layout))

	// Try to bound heap growth.
	if gcTotalAlloc+uint64(size) < gcTotalAlloc {
		abort()
	}

	// Allocate the memory.
	pointer := extalloc(size)
	if pointer == nil {
		gcAllocPanic()
	}

	// Zero-out the allocated memory
	memzero(pointer, size)

	// Update used memory
	gcTotalAlloc += uint64(size)

	return pointer
}

// memory is never freed from the GC
func free(ptr unsafe.Pointer) {}

func markRoots(start, end uintptr) {}

func GC() {}

// SetFinalizer registers a finalizer.
func SetFinalizer(obj interface{}, finalizer interface{}) {}

// ReadMemStats populates m with memory statistics.
func ReadMemStats(m *MemStats) {
	m.HeapIdle = 0
	m.HeapInuse = gcTotalAlloc
	m.HeapReleased = 0 // always 0, we don't currently release memory back to the OS.
	m.HeapSys = m.HeapInuse + m.HeapIdle
	m.GCSys = 0
	m.TotalAlloc = gcTotalAlloc
	m.Mallocs = gcMallocs
	m.Frees = gcFrees
	m.Sys = uint64(heapEnd - heapStart)
}
