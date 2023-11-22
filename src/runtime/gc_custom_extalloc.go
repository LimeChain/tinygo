//go:build gc.custom

package runtime

// This is an implementation of a conservative, tracing (mark and sweep) garbage collector,
// that relies on external memory allocator for the WebAssembly (polkawasm) target.

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

import (
	"unsafe"
)

// Flag used to detect if the collector is invoking itself recursively.
var gcCollectorRunning bool

// Total number of calls to alloc().
var gcMallocs uint64

// Total number of calls to free().
var gcFrees uint64

// Total amount of memory allocated on the heap, used
// to bound the heap size.
var gcTotalAlloc uint64

// Controls the growth of the heap. When the heap exceeds
// this size, the garbage collector is run. If the garbage
// collector cannot free up enough memory, the bound is
// doubled until the allocation fits.
//
// Size of a pointer on the target architecture (machine word size).
var heapBound uintptr = 4 * unsafe.Sizeof(unsafe.Pointer(nil))

// Gets returned when allocating zero-sized memory
// to avoid returning nil pointers.
var zeroSizedAlloc uint8

// List of all allocations on the heap.
var allocations []allocEntry

// Queue of marked allocations that need to be scanned.
var scanQueue *allocEntry

// Representation of single heap allocation. Used to keep track
// of all allocations and to find references between them.
// start, end - object header (meta data) + data + padding
// next - next allocation in the list of allocations
type allocEntry struct {
	start, end uintptr
	next       *allocEntry
}

// Representation of a slice header (without the backing array).
type SliceHeader struct {
	ptr      unsafe.Pointer
	len, cap uintptr
}

// The heap is initialized by the external allocator at program start.
func initHeap() {}

// The heap is in custom GC, so ignore it when called from wasm initialization
func setHeapEnd(newHeapEnd uintptr) {}

// Tries to find free space on the heap to allocate memory of the given size
// and returns a pointer to it, possibly doing a garbage collection
// cycle if needed. If no space is free, it panics
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	if gcCollectorRunning {
		gcRunningPanic()
	}

	gcMallocs++

	if size == 0 {
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	size += align(unsafe.Sizeof(layout))

	var gcRan bool
	for {
		// Try to bound the heap size.
		if gcTotalAlloc+uint64(size) < gcTotalAlloc {
			abort()
		}

		if gcTotalAlloc+uint64(size) > uint64(heapBound) {
			if !gcRan {
				// Run the garbage collector before growing the heap.
				GC()
				gcRan = true
				continue
			} else {
				// Grow the heap bound to fit the allocation.
				for heapBound != 0 && uintptr(gcTotalAlloc)+size > heapBound {
					heapBound <<= 1
				}
				if heapBound == 0 {
					// This is only possible on hosted 32-bit systems.
					// Allow the heap bound to encompass everything.
					heapBound = ^uintptr(0)
				}
			}
		}

		// Keep a copy of the allocations list header
		allocationsHeader := *getSliceHeader(&allocations)

		// Ensure that there is enough space in the alloc list for the new allocations.
		if allocationsHeader.len == allocationsHeader.cap {
			// oldAllocations := allocations

			// Attempt to double the capacity of the alloc list.
			newCap := uintptr(double(int(allocationsHeader.cap)))

			// Allocate new memory for the expanded slice
			newAllocationsPtr := extalloc(newCap * allocEntrySize())
			if newAllocationsPtr == nil {
				if gcRan {
					// Garbage collector was not able to free up enough memory.
					gcAllocPanic()
				} else {
					// Run the garbage collector and try again.
					GC()
					gcRan = true
					continue
				}
			}

			newAllocationsHeader := getSliceHeader(&allocations)
			setSliceHeader(newAllocationsHeader, newAllocationsPtr, allocationsHeader.len, newCap)

			// Copy the old slice into the new slice
			// memcpy(newAllocationsHeader.ptr, (&allocationsHeader).ptr, (&allocationsHeader).len)
			copyAllocations(newAllocationsHeader, &allocationsHeader) // copy(allocations, oldAllocations)

			if allocationsHeader.cap != 0 {
				free(allocationsHeader.ptr) // unsafe.Pointer(&oldAllocations[0])
			}
		}

		// Allocate the memory from the external allocator
		ptr := extalloc(size)
		if ptr == nil {
			if gcRan {
				// Garbage collector was not able to free up enough memory.
				gcAllocPanic()
			} else {
				// Run the garbage collector and try again.
				GC()
				gcRan = true
				continue
			}
		}

		// Add the allocation to the list of allocations
		// i := len(allocations); allocations = allocations[:i+1]; allocations[i] = newAllocEntry(ptr, size);
		newAllocationsHeader := getSliceHeader(&allocations)
		appendAllocations(newAllocationsHeader, newAllocEntry(ptr, size))

		// Zero-out the allocated memory
		memzero(ptr, size)

		// Update the total allocated memory
		gcTotalAlloc += uint64(size)

		// Return the pointer to the allocated memory
		return ptr
	}
}

// Excplicitly frees previously allocated pointer.
func free(ptr unsafe.Pointer) {
	gcFrees++

	// Free the memory
	extfree(ptr)
}

// Scans for references on the stack and marks all reachable allocations.
// It is currently only called with the top and bottom of the stack
func markRoots(start, end uintptr) {
	scan(start, end)
}

// func markRoot(addr, root uintptr) {
// 	mark(root)
// }

// Performs a garbage collection cycle (mark and sweep).
func GC() {
	if gcCollectorRunning {
		gcRunningPanic()
	}
	gcCollectorRunning = true

	// The heap is empty
	if len(allocations) == 0 {
		gcCollectorRunning = false
		return
	}

	// Sort the allocation list so that it can be searched efficiently
	sortAllocations()

	// Check that the allocation list is sorted
	checkIfAllocationsSorted()

	// Unmark all allocations in the allocation list
	unmarkAllocations()

	// Reset the scan queue
	scanQueue = nil

	// Scan the stack and mark all reachable allocations that are referenced by the stack roots (markStack).
	markStack()

	findGlobals(markRoots)

	// 	var markedTaskQueue task.Queue
	// 	// Channel operations in interrupts may move task pointers around while we are marking.
	// 	// Therefore we need to scan the runqueue seperately.
	// runqueueScan:
	// 	for !runqueue.Empty() {
	// 		// Pop the next task off of the runqueue.
	// 		t := runqueue.Pop()

	// 		// Mark the task if it has not already been marked.
	// 		markRoot(uintptr(unsafe.Pointer(&runqueue)), uintptr(unsafe.Pointer(t)))

	// 		// Push the task onto our temporary queue.
	// 		markedTaskQueue.Push(t)
	// 	}

	finishMarking()

	// // Restore the runqueue.
	// i := interrupt.Disable()
	// if !runqueue.Empty() {
	// 	// Something new came in while finishing the mark.
	// 	interrupt.Restore(i)
	// 	goto runqueueScan
	// }
	// runqueue = markedTaskQueue
	// interrupt.Restore(i)

	// Remove and free all remaining unmarked allocations.
	sweep()

	gcCollectorRunning = false
}

// SetFinalizer registers a finalizer
func SetFinalizer(obj interface{}, finalizer interface{}) {}

// Populates the given MemStats struct with memory statistics
func ReadMemStats(m *MemStats) {
	m.HeapIdle = 0
	m.HeapInuse = gcTotalAlloc
	m.HeapReleased = 0 // always 0, we don't currently release memory back to the OS.

	// ms.Sys = uint64(heapEnd - heapStart)
	m.HeapSys = m.HeapInuse + m.HeapIdle
	m.GCSys = 0
	m.TotalAlloc = gcTotalAlloc
	m.Mallocs = gcMallocs
	m.Frees = gcFrees
}

// Scans all pointer-aligned words in a given range and marks any
// pointers (allocations they point to) that it finds. Performs a
// conservative scan, so it may mark allocations that are not
// actually referenced.
func scan(start, end uintptr) {
	alignment := unsafe.Alignof(unsafe.Pointer(nil))

	// Align the start pointer to the next pointer-aligned word.
	start = (start + alignment - 1) &^ (alignment - 1)

	// Mark all pointer aligned words in the given range.
	for addr := start; addr+unsafe.Sizeof(unsafe.Pointer(nil)) <= end; addr += alignment {
		mark(*(*uintptr)(unsafe.Pointer(addr)))
	}

	// // Mark the last word in the given range if it is pointer-aligned.
	// if end&^(alignment-1) == end {
	// 	mark(*(*uintptr)(unsafe.Pointer(end)))
	// }
}

// Searches the allocation list for the given address (allocation containing the given address)
// and marks the corresponding allocation if found
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

	if curAlloc != nil && curAlloc.next == nil {
		// Push the allocation onto the scan queue
		nextAlloc := scanQueue

		if nextAlloc == nil {
			// Insert a loop, so we can tell that this isn't marked
			nextAlloc = curAlloc
		}

		scanQueue, curAlloc.next = curAlloc, nextAlloc

		return true
	}

	// The address does not reference an unmarked allocation
	return false
}

func finishMarking() {
	// Scan all allocations that are referenced by the scan queue.
	// This is done in a loop, because scanning an allocation may
	// add more allocations to the scan queue.
	for scanQueue != nil {
		// Pop a marked allocation off of the scan queue.
		curAlloc := scanQueue
		nextAlloc := curAlloc.next

		// This is the last value on the queue.
		if nextAlloc == curAlloc {
			nextAlloc = nil
		}

		scanQueue = nextAlloc

		// Scan and mark all allocations that are referenced
		// by this allocation and adds them to the scan queue.
		scan(curAlloc.start, curAlloc.end)
	}
}

func sweep() {
	gcTotalAlloc = 0
	j := 0

	for _, curAlloc := range allocations {
		// This was never marked
		if curAlloc.next == nil {
			free(unsafe.Pointer(curAlloc.start))
			continue
		}
		// Move this down in the list
		allocations[j] = curAlloc
		j++

		// Re-calculate used memory.
		gcTotalAlloc += uint64(curAlloc.end - curAlloc.start)
	}

	// 2. Remove the allocation from the list of allocations
	allocations = allocations[:j]
}

// Unmarks all allocations in the allocation list.
func unmarkAllocations() {
	for i := range allocations {
		allocations[i].next = nil
	}
}

// Checks if the allocation list is sorted by start address.
func checkIfAllocationsSorted() {
	if len(allocations) > 1 {
		for i := range allocations[1:] {
			if allocations[i+1].start < allocations[i].start { // <=
				gcUnsortedAllocsPanic()
			}
		}
	}
}

// Sorts the allocation list by using heapsort (in-place)
//
//go:noinline
func sortAllocations() {
	// Turn the array into a max heap
	for i := len(allocations)/2 - 1; i >= 0; i-- {
		heapify(allocations, len(allocations), i)
	}

	// Heap sort
	for end := len(allocations) - 1; end > 0; end-- {
		// Swap the max element with the last item
		allocations[0], allocations[end] = allocations[end], allocations[0]
		// Restore the heap property
		heapify(allocations, end, 0)
	}
}

// heapify corrects the heap structure assuming children of i are already heaps
func heapify(arr []allocEntry, n, i int) {
	max := i
	left := 2*i + 1
	right := 2*i + 2

	if left < n && arr[left].start > arr[max].start {
		max = left
	}

	if right < n && arr[right].start > arr[max].start {
		max = right
	}

	if max != i {
		arr[i], arr[max] = arr[max], arr[i]
		heapify(arr, n, max)
	}
}

// Searches the allocations for the given address.
// If the address is found in an allocation, a pointer
// to the corresponding entry is returned.
func searchAllocations(addr uintptr) *allocEntry {
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

// Returns a slice header for the given slice pointer.
func getSliceHeader(list *[]allocEntry) *SliceHeader {
	return (*SliceHeader)(unsafe.Pointer(list))
}

// Set the slice header to the given values.
func setSliceHeader(header *SliceHeader, ptr unsafe.Pointer, len, cap uintptr) {
	header.ptr = ptr
	header.len = len
	header.cap = cap
}

// Gets an allocation entry at the given offset in the allocations list.
func getAllocationsEntry(header *SliceHeader, offset uintptr) allocEntry {
	offsetPtr := unsafe.Add(header.ptr, offset*allocEntrySize())
	return *(*allocEntry)(offsetPtr)
}

// Sets an allocation entry at the given offset in the allocations list.
func setAllocationsEntry(header *SliceHeader, offset uintptr, entry allocEntry) {
	offsetPtr := unsafe.Add(header.ptr, offset*allocEntrySize()) // unsafe.Sizeof(entry)
	*(*allocEntry)(offsetPtr) = entry
}

// Appends an entry to the end of the allocations list.
func appendAllocations(header *SliceHeader, entry allocEntry) {
	offset := header.len
	setAllocationsEntry(header, offset, entry)
	header.len++
	*(*SliceHeader)(unsafe.Pointer(&allocations)) = *header
}

// Copies all allocations from the source slice to the destination slice.
func copyAllocations(dstHeader *SliceHeader, srcHeader *SliceHeader) {
	for i := 0; i < int(srcHeader.len); i++ {
		entry := getAllocationsEntry(srcHeader, uintptr(i))
		setAllocationsEntry(dstHeader, uintptr(i), entry)
	}
}

// Initializes new allocation entry with the given pointer and size.
func newAllocEntry(ptr unsafe.Pointer, size uintptr) allocEntry {
	return allocEntry{
		start: uintptr(ptr),
		end:   uintptr(ptr) + size,
	}
}

// Size of single allocation entry.
func allocEntrySize() uintptr {
	return unsafe.Sizeof(allocEntry{})
}

func double(value int) int {
	if value == 0 {
		return 1
	}
	return 2 * value
}
