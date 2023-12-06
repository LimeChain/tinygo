//go:build gc.extalloc

package runtime

// This is an implementation of a conservative, tracing (mark-sweep)
// garbage collector, that relies on external memory allocator for the
// WebAssembly (polkawasm) target.

import (
	"unsafe"
)

var (
	// Used to detect whether the garbage collector is invoking
	// itself recursively.
	gcInProgress bool

	// Total number of calls to alloc() / free()
	gcMallocs, gcFrees uint64

	// Total amount of memory allocated on the heap, used to limit
	// the heap size.
	gcTotalAlloc uint64

	// Controls the heap growth and when to run the garbage collector.
	heapUsageLimit uintptr = 2 * wasmPageSize * unsafe.Sizeof(unsafe.Pointer(nil))

	// Returned for zero-sized allocations to avoid returning nil pointers.
	zeroSizedAlloc uint8

	// TODO: employ a more efficient data structure (insertion/deletion/lookup time complexity)
	// List of all allocations on the heap.
	allocations []heapAllocation

	// Queue of marked allocations (considered "live") that need to be
	// scanned. Contains allocations that are referenced by the stack,
	// globals, etc. and need to be scanned for references to other allocations.
	referenceScanQueue *heapAllocation
)

// Representation of single heap allocation. Used to keep track
// of all allocations and to find references between them.
type heapAllocation struct {
	start, end uintptr // header + data + padding
	marked     bool
	next       *heapAllocation // used by the scan queue
}

// Representation of a slice header (without the backing array).
type sliceHeader struct {
	ptr      unsafe.Pointer
	len, cap uintptr
}

// The heap is initialized by the external allocator.
func initHeap() {}

func setHeapEnd(newHeapEnd uintptr) {}

// Tries to find free space on the heap to allocate memory of the
// given size and returns a pointer to it, possibly doing a garbage
// collection cycle if needed. If no space is free, it panics.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	// Ensure not in a recursive GC call.
	if gcInProgress {
		gcRunningPanic()
	}

	gcMallocs++

	// Handle zero-sized allocation by returning a non-nil pointer.
	if size == 0 {
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	// Align the size to the next pointer-aligned word.
	size += align(unsafe.Sizeof(layout))

	// Try to allocate memory until it succeeds or the garbage collector
	// cannot free up enough memory.
	var gcRan bool
	for {
		// Runs a garbage collection cycle if needed and adjusts the heap
		// usage limit. If the garbage collector cannot free up enough memory,
		// the limit is doubled until the allocation fits.
		if needGC(size) {
			tryGC(&gcRan, size, expandHeap)
			continue
		}

		// Ensures that there is enough space in the allocations list
		// for the new allocation.
		expandAllocationsIfNeeded(&gcRan)

		// Allocate memory with the requested size.
		ptr := extalloc(size)
		if ptr == nil {
			// Runs a garbage collection cycle if needed and panics if the
			// garbage collector cannot free up enough memory.
			tryGC(&gcRan, size, fail)
			continue
		}

		// Update the allocations list with the new allocation.
		appendAllocations(getSliceHeader(&allocations), newHeapAllocation(ptr, size))

		// Update the total allocated memory.
		gcTotalAlloc += uint64(size)

		// Zero-out the allocated memory.
		memzero(ptr, size)

		// Return the pointer to the allocated memory
		return ptr
	}
}

// Explicitly frees previously allocated memory.
//
//go:noinline
func free(ptr unsafe.Pointer) {
	gcFrees++
	extfree(ptr)
}

// Scans for references on the stack and marks all reachable allocations.
// It is currently only called with the top and bottom of the stack
//
//go:noinline
func markRoots(start, end uintptr) {
	scan(start, end)
}

func markRoot(addr, root uintptr) {
	mark(root)
}

// Performs a garbage collection cycle.
//
//go:noinline
func GC() {
	if gcInProgress {
		gcRunningPanic()
	}
	gcInProgress = true

	// The heap is empty.
	if len(allocations) == 0 {
		gcInProgress = false
		return
	}

	prepareGC()

	// Scan the stack and mark all reachable allocations that are
	// referenced by the stack roots.
	markStack()

	// Scan the globals and mark all reachable allocations that are
	// referenced by the globals.
	findGlobals(markRoots)

	// scheduler is disabled

	finishMarking()

	// Remove and free all remaining unmarked allocations.
	sweep()

	gcInProgress = false
}

// SetFinalizer registers a finalizer.
func SetFinalizer(obj interface{}, finalizer interface{}) {}

// ReadMemStats populates m with memory statistics.
func ReadMemStats(m *MemStats) {
	m.HeapIdle = 0
	m.HeapInuse = gcTotalAlloc
	m.HeapReleased = 0 // always 0, we don't currently release memory back to the OS.
	m.Sys = uint64(heapEnd - heapStart)
	m.HeapSys = m.HeapInuse + m.HeapIdle
	m.GCSys = 0
	m.TotalAlloc = gcTotalAlloc
	m.Mallocs = gcMallocs
	m.Frees = gcFrees
}

// Returns true if the heap needs to be garbage collected.
//
//go:inline
func needGC(size uintptr) bool {
	return gcTotalAlloc+uint64(size) > uint64(heapUsageLimit)
}

// Tries to run a GC cycle and then performs an action based on the callback.
func tryGC(gcRan *bool, size uintptr, fn func(bool, uintptr)) {
	if *gcRan {
		// If the garbage collector has already run, perform the action.
		fn(true, size)
	} else {
		// Run the garbage collector and update the flag.
		GC()
		*gcRan = true
	}
}

// Expands the heap.
//
//go:inline
func expandHeap(gcRan bool, size uintptr) {
	if gcRan {
		adjustHeapUsageLimit(size)
	}
}

// Handles failure in memory allocation.
//
//go:inline
func fail(gcRan bool, size uintptr) {
	if gcRan {
		gcAllocPanic()
	}
}

// Increases the heapUsageLimit to accommodate the allocation size.
//
//go:inline
func adjustHeapUsageLimit(size uintptr) {
	// Grow the heap usage limit until the allocation fits.
	for heapUsageLimit != 0 && uintptr(gcTotalAlloc)+size > heapUsageLimit {
		heapUsageLimit <<= 1
	}
	if heapUsageLimit == 0 {
		// This is only possible on hosted 32-bit systems.
		// Allow the heap limit to encompass everything.
		heapUsageLimit = ^uintptr(0)
	}
}

// Ensure that there is enough space in the allocations list
// for the new allocations, resize if needed.
//
//go:noinline
func expandAllocationsIfNeeded(gcRan *bool) {
	// Keep a copy of the current allocations list header.
	allocationsHeader := *getSliceHeader(&allocations)

	// Check if the allocations list is full, if so, attempt to double its capacity.
	if len(allocations) == cap(allocations) {
		doubledCap := uintptr(double(int(allocationsHeader.cap)))

		// Allocate new memory for the underlying array of the allocations list.
		ptr := extalloc(doubledCap * heapAllocationSize())
		if ptr == nil {
			tryGC(gcRan, 0, fail)
		}

		// Create a new slice header for the allocations list.
		newAllocationsHeader := getSliceHeader(&allocations)
		setSliceHeader(newAllocationsHeader, ptr, allocationsHeader.len, doubledCap)

		// Copy the old allocations to the new allocations list.
		copyAllocations(newAllocationsHeader, &allocationsHeader)
		// TODO:
		// memcpy(newAllocationsHeader.ptr, allocationsHeader.ptr, allocationsHeader.len)

		// Free the old allocations list.
		if allocationsHeader.cap != 0 {
			free(allocationsHeader.ptr)
		}
	}
}

// Returns a slice header for the given slice pointer.
//
//go:inline
func getSliceHeader(list *[]heapAllocation) *sliceHeader {
	return (*sliceHeader)(unsafe.Pointer(list))
}

// Set the slice header to the given values.
//
//go:inline
func setSliceHeader(header *sliceHeader, ptr unsafe.Pointer, len, cap uintptr) {
	header.ptr = ptr
	header.len = len
	header.cap = cap
}

// Gets an allocation at the given offset in the allocations list.
//
//go:inline
func getAllocationsEntry(header *sliceHeader, offset uintptr) heapAllocation {
	offsetPtr := unsafe.Add(header.ptr, offset*heapAllocationSize())
	return *(*heapAllocation)(offsetPtr)
}

// Sets an allocation at the given offset in the allocations list.
//
//go:inline
func setAllocationsEntry(header *sliceHeader, offset uintptr, entry heapAllocation) {
	offsetPtr := unsafe.Add(header.ptr, offset*heapAllocationSize())
	*(*heapAllocation)(offsetPtr) = entry
}

// Copies all allocations from the source slice to the destination slice.
func copyAllocations(dstHeader *sliceHeader, srcHeader *sliceHeader) {
	for i := 0; i < int(srcHeader.len); i++ {
		entry := getAllocationsEntry(srcHeader, uintptr(i))
		setAllocationsEntry(dstHeader, uintptr(i), entry)
	}
}

// Appends an allocation to the end of the allocations list.
func appendAllocations(header *sliceHeader, entry heapAllocation) {
	offset := header.len
	setAllocationsEntry(header, offset, entry)
	header.len++
}

// Initializes new allocation with the given pointer and size.
//
//go:inline
func newHeapAllocation(ptr unsafe.Pointer, size uintptr) heapAllocation {
	return heapAllocation{
		start: uintptr(ptr),
		end:   uintptr(ptr) + size,
	}
}

// Size of single allocation.
//
//go:inline
func heapAllocationSize() uintptr {
	return unsafe.Sizeof(heapAllocation{})
}

//go:noinline
func prepareGC() {
	// Sort the allocation list so that it can be searched efficiently.
	sortAllocations()

	// Unmark all allocations in the allocations list.
	unmarkAllocations()

	// Reset the scan queue.
	referenceScanQueue = nil
}

// Scans all pointer-aligned words in a given range and marks any
// pointers (allocations they point to) that it finds. Performs a
// conservative scan, so it may mark allocations that are not
// actually referenced.
//
//go:noinline
func scan(start, end uintptr) {
	alignment := unsafe.Alignof(unsafe.Pointer(nil))

	// Align the start pointer to the next pointer-aligned word.
	start = (start + alignment - 1) &^ (alignment - 1)

	// Mark all pointer aligned words in the given range.
	for addr := start; addr+unsafe.Sizeof(unsafe.Pointer(nil)) <= end; addr += alignment {
		mark(*(*uintptr)(unsafe.Pointer(addr)))
	}

	// Mark the last word in the given range if it is a pointer.
	if end >= start+unsafe.Sizeof(unsafe.Pointer(nil)) {
		mark(*(*uintptr)(unsafe.Pointer(end - unsafe.Sizeof(unsafe.Pointer(nil)))))
	}
}

// Searches the allocation list for the given address (allocation containing the given address)
// and marks the corresponding allocation if found
//
//go:noinline
func mark(addr uintptr) bool {
	// The heap is empty
	if len(allocations) == 0 {
		return false
	}

	if addr < allocations[0].start || addr > allocations[len(allocations)-1].end {
		// Pointer is outside of allocated bounds.
		return false
	}

	// Search the allocation list for the address and mark it if found
	curAlloc := searchAllocations(addr)
	if curAlloc != nil && !curAlloc.marked {
		curAlloc.marked = true
		// Push the allocation onto the scan queue to scan it later.
		curAlloc.next = referenceScanQueue
		referenceScanQueue = curAlloc
		return true
	}

	// The address does not reference an unmarked allocation.
	return false
}

func finishMarking() {
	// Scan all allocations that are referenced by the scan queue.
	// This is done in a loop, because scanning an allocation may
	// add more allocations to the scan queue.
	for referenceScanQueue != nil {
		// Pop a marked allocation off of the scan queue.
		curAlloc := referenceScanQueue
		referenceScanQueue = curAlloc.next
		// Scan and mark all allocations that are referenced
		// by this allocation and adds them to the scan queue.
		scan(curAlloc.start, curAlloc.end)
	}
}

//go:noinline
func sweep() {
	j := 0
	gcTotalAlloc = 0

	for i := range allocations {
		curAlloc := allocations[i]

		if !curAlloc.marked {
			// This was never marked during the scan, so it is unreachable.
			free(unsafe.Pointer(curAlloc.start))
			continue
		}

		// Move this down in the list to keep it compact.
		allocations[j] = curAlloc
		j++
		gcTotalAlloc += uint64(curAlloc.end - curAlloc.start)
	}

	allocations = allocations[:j]
}

// Unmarks all allocations in the allocations list.
//
//go:inline
func unmarkAllocations() {
	for i := range allocations {
		allocations[i].marked = false
		allocations[i].next = nil
	}
}

// Sorts the allocation list by using heapsort for efficient searching.
//
//go:noinline
func sortAllocations() {
	// Turn the array into a max heap

	n := len(allocations)

	for i := n/2 - 1; i >= 0; i-- {
		heapify(allocations, n, i)
	}

	for end := n - 1; end > 0; end-- {
		// move current root to end
		allocations[0], allocations[end] = allocations[end], allocations[0]

		heapify(allocations, end, 0)
	}
}

// Corrects the heap structure assuming children of i are already heaps.
// This function is a part of the heap sort algorithm used in sortAllocations.
// It ensures that the subtree rooted at index i is a max-heap.
//
//go:noinline
func heapify(arr []heapAllocation, n, i int) {
	for {
		max := i
		left := 2*i + 1
		right := 2*i + 2

		// left is larger than root
		if left < n && arr[left].start > arr[max].start {
			max = left
		}

		// right is larger than largest
		if right < n && arr[right].start > arr[max].start {
			max = right
		}

		// largest is not root
		if max != i {
			arr[i], arr[max] = arr[max], arr[i]
			i = max
		} else {
			break
		}
	}
}

// Searches the allocations for the given address.
// If the address is found in an allocation, a pointer
// to the corresponding allocation is returned.
//
//go:inline
func searchAllocations(addr uintptr) *heapAllocation {
	low, high := 0, len(allocations)

	for low < high {
		mid := low + (high-low)/2

		if addr < allocations[mid].start {
			high = mid
		} else if addr > allocations[mid].end {
			low = mid + 1
		} else {
			return &allocations[mid]
		}
	}

	return nil
}

//go:inline
func double(value int) int {
	if value == 0 {
		return 1
	}
	return 2 * value
}
