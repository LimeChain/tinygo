//go:build gc.custom
// +build gc.custom

package runtime

// This GC strategy allows an external GC to be plugged in instead of the builtin
// implementations.
//
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

const gcDebug = false

//go:noinline
func printnum(num int) {
	digits := [10]int{}

	for i := 0; num > 0; i++ {
		digit := num % 10
		digits[i] = digit
		num = num / 10
	}

	for i := 0; i < len(digits)/2; i++ {
		j := len(digits) - i - 1
		digits[i], digits[j] = digits[j], digits[i]
	}

	skipZeros := true
	for i := 0; i < len(digits); i++ {
		digit := digits[i]
		if skipZeros && digit == 0 {
			continue
		}
		skipZeros = false

		digitStr := ""

		switch digit {
		case 0:
			digitStr = "0"
		case 1:
			digitStr = "1"
		case 2:
			digitStr = "2"
		case 3:
			digitStr = "3"
		case 4:
			digitStr = "4"
		case 5:
			digitStr = "5"
		case 6:
			digitStr = "6"
		case 7:
			digitStr = "7"
		case 8:
			digitStr = "8"
		case 9:
			digitStr = "9"
		default:
		}

		printstr(digitStr)
	}
}

//go:noinline
func printstr(str string) {
	if !gcDebug {
		return
	}

	for i := 0; i < len(str); i++ {
		if putcharPosition >= putcharBufferSize {
			break
		}

		putchar(str[i])
	}
}

// Total amount allocated for runtime.MemStats
var gcTotalAlloc uint64

// Total number of calls to alloc()
var gcMallocs uint64

// Total number of objected freed; for leaking collector this stays 0
const gcFrees = 0

// zeroSizedAlloc is just a sentinel that gets returned when allocating 0 bytes.
var zeroSizedAlloc uint8

// alloc is called to allocate memory. layout is currently not used.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	printstr("alloc(")
	printnum(int(size))
	printstr(")\n")

	if size == 0 {
		printstr("zero-size allocation\n")
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	size = align(size)
	gcMallocs++

	// try to bound heap growth
	if gcTotalAlloc+uint64(size) < gcTotalAlloc {
		printstr("\tout of memory\n")
		abort()
	}

	// allocate memory
	pointer := extalloc(size)
	if pointer == nil {
		printstr("\textalloc call failed\n")
		abort()
	}

	// zero-out the allocated memory
	memzero(pointer, size)
	gcTotalAlloc += uint64(size)
	return pointer
}

// free is called to explicitly free a previously allocated pointer.
func free(ptr unsafe.Pointer) {
	extfree(ptr)
}

// GC is called to explicitly run garbage collection.
func GC() {

}

// SetFinalizer registers a finalizer.
func SetFinalizer(obj interface{}, finalizer interface{}) {

}

// initHeap is called when the heap is first initialized at program start.
func initHeap() {
	// heap is initialized by the external allocator
}

func setHeapEnd(newHeapEnd uintptr) {
	// Heap is in custom GC so ignore for when called from wasm initialization.
}

// markRoots is called with the start and end addresses to scan for references.
// It is currently only called with the top and bottom of the stack.
func markRoots(start, end uintptr) {

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
