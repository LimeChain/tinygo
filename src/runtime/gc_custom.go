//go:build gc.custom

package runtime

// Simple GC, that calls an external allocator. It does not free memory, but works faster
// and memory is freed at the end of the execution from the external allocator.

// The interface defined in this file is not stable and can be broken at anytime, even
// across minor versions.
//
// runtime.markStack() must be called at the beginning of any GC cycle. //go:linkname
// on a function without a body can be used to access this internal function.
//
// The custom implementation must provide the following functions in the runtime package
// using the go:linkname directive:
//
// - func initHeap()
// - func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer
// - func free(ptr unsafe.Pointer)
// - func markRoots(start, end uintptr)
// - func GC()
// - func SetFinalizer(obj interface{}, finalizer interface{})
// - func ReadMemStats(ms *runtime.MemStats)
//
//
// In addition, if targeting wasi, the following functions should be exported for interoperability
// with wasi libraries that use them. Note, this requires the export directive, not go:linkname.
//
// - func malloc(size uintptr) unsafe.Pointer
// - func free(ptr unsafe.Pointer)
// - func calloc(nmemb, size uintptr) unsafe.Pointer
// - func realloc(oldPtr unsafe.Pointer, size uintptr) unsafe.Pointer

import (
	"unsafe"
)

// Total amount allocated for runtime.MemStats
var gcTotalAlloc uint64

// Total number of calls to alloc()
var gcMallocs uint64

// Total number of objected freed; for leaking collector this stays 0
const gcFrees = 0

// zeroSizedAlloc is just a sentinel that gets returned when allocating 0 bytes.
var zeroSizedAlloc uint8

// initHeap is called when the heap is first initialized at program start.
func initHeap() {
	// Heap is initialized by the external allocator
}

func setHeapEnd(newHeapEnd uintptr) {
	// Heap is in custom GC, so ignore it when called from wasm initialization.
}

// alloc tries to find free space on the heap to allocate memory,
// If no space is free, it panics. layout is currently not used.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	printstr("call alloc(")
	printnum(int(size))
	printstr(")\n")

	if size == 0 {
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	printstr("\ttotal memory ")
	printnum(int(gcTotalAlloc))
	printstr("\n")

	size = align(size)

	// Try to bound heap growth.
	if gcTotalAlloc+uint64(size) < gcTotalAlloc {
		printstr("\tout of memory\n")
		abort()
	}

	// Allocate the memory.
	pointer := extalloc(size)
	if pointer == nil {
		printstr("\textalloc call failed\n")
		abort()
	}

	// Zero-out the allocated memory
	memzero(pointer, size)

	// Update used memory
	gcTotalAlloc += uint64(size)

	return pointer
}

// free is called to explicitly free a previously allocated pointer.
func free(ptr unsafe.Pointer) {
	// memory is never freed from the GC, but from the
	// external allocator at the end of the execution
}

// markRoots is called with the start and end addresses to scan for references.
// It is currently only called with the top and bottom of the stack.
func markRoots(start, end uintptr) {

}

// GC is called to explicitly run garbage collection.
func GC() {

}

// SetFinalizer registers a finalizer.
func SetFinalizer(obj interface{}, finalizer interface{}) {

}

// ReadMemStats populates m with memory statistics.
func ReadMemStats(ms *MemStats) {
	ms.HeapIdle = 0
	ms.HeapInuse = gcTotalAlloc
	ms.HeapReleased = 0 // always 0, we don't currently release memory back to the OS.

	ms.HeapSys = ms.HeapInuse + ms.HeapIdle
	ms.GCSys = 0
	ms.TotalAlloc = gcTotalAlloc
	ms.Mallocs = gcMallocs
	ms.Frees = gcFrees
	ms.Sys = uint64(heapEnd - heapStart)
}
