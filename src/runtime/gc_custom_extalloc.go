//go:build gc.custom_wip

package runtime

// WIP

// This is a conservative collector which uses an external memory allocator.
// It keeps a list of allocations for tracking purposes.

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

// Total amount allocated for runtime.MemStats
var gcTotalAlloc uint64

// Total number of calls to alloc()
var gcMallocs uint64

// Total number of objected freed; for leaking collector this stays 0
var gcFrees uint64

// This is used to detect if the collector is invoking itself or trying to allocate memory.
var gcRunning bool

// heapBound is used to control the growth of the heap.
// When the heap exceeds this size, the garbage collector is run.
// If the garbage collector cannot free up enough memory, the bound is doubled until the allocation fits.
var heapBound uintptr = 4 * unsafe.Sizeof(unsafe.Pointer(nil))

// zeroSizedAlloc is just a sentinel that gets returned when allocating 0 bytes.
var zeroSizedAlloc uint8

// scanQueue is a queue of marked allocations to scan.
var scanQueue *allocListEntry

var allocList []allocListEntry

// allocListEntry is a listing of a single heap allocation.
type allocListEntry struct {
	start uintptr
	end   uintptr
	next  *allocListEntry
}

// scan marks all allocations referenced by this allocation.
// This should only be invoked by the garbage collector.
func (e *allocListEntry) scan() {
	scan(e.start, e.end)
}

// scan loads all pointer-aligned words and marks any pointers that it finds.
func scan(start uintptr, end uintptr) {
	// Align start pointer.
	start = (start + unsafe.Alignof(unsafe.Pointer(nil)) - 1) &^ (unsafe.Alignof(unsafe.Pointer(nil)) - 1)

	// Mark all pointers.
	for ptr := start; ptr+unsafe.Sizeof(unsafe.Pointer(nil)) <= end; ptr += unsafe.Alignof(unsafe.Pointer(nil)) {
		mark(*(*uintptr)(unsafe.Pointer(ptr)))
	}
}

// mark searches for an allocation containing the given address and marks it if found.
func mark(addr uintptr) bool {
	if len(allocList) == 0 {
		// The heap is empty.
		return false
	}

	if addr < allocList[0].start || addr > allocList[len(allocList)-1].end {
		// Pointer is outside of allocated bounds.
		return false
	}

	// Search the allocation list for this address.
	alloc := searchAllocList(allocList, addr)
	if alloc != nil && alloc.next == nil {
		printstr("mark ")
		printnum(int(addr))
		printstr("\n")

		// Push the allocation onto the scan queue.
		next := scanQueue
		if next == nil {
			// Insert a loop so we can tell that this isn't marked.
			next = alloc
		}
		scanQueue, alloc.next = alloc, next

		return true
	}

	// The address does not reference an unmarked allocation.
	return false
}

// searchAllocList searches a sorted alloc list for an address.
// If the address is found in an allocation, a pointer to the corresponding entry is returned.
// Otherwise, this returns nil.
func searchAllocList(list []allocListEntry, addr uintptr) *allocListEntry {
	for len(list) > 0 {
		mid := len(list) / 2
		switch {
		case addr < list[mid].start:
			list = list[:mid]
		case addr > list[mid].end:
			list = list[mid+1:]
		default:
			return &list[mid]
		}
	}

	return nil
}

// sortAllocList sorts an allocation list using heapsort.
//
//go:noinline
func sortAllocList(list []allocListEntry) {
	// Turn the array into a max heap.
	for i, v := range list {
		// Repeatedly swap v up the heap until the node above is at a greater address (or the top of the heap is reached).
		for i > 0 && v.start > list[(i-1)/2].start {
			list[i] = list[(i-1)/2]
			i = (i - 1) / 2
		}
		list[i] = v
	}

	// Repeatedly remove the max and place it at the end of the array.
	for len(list) > 1 {
		// Remove the max and place it at the end of the array.
		list[0], list[len(list)-1] = list[len(list)-1], list[0]
		list = list[:len(list)-1]

		// Fix the position of the element we swapped into the root.
		i := 0
		for {
			// Find the element that should actually be at this position.
			max := i
			if l := 2*i + 1; l < len(list) && list[l].start > list[max].start {
				max = l
			}
			if r := 2*i + 2; r < len(list) && list[r].start > list[max].start {
				max = r
			}

			if max == i {
				// The element is where it is supposed to be.
				break
			}

			// Swap this element down the heap.
			list[i], list[max] = list[max], list[i]
			i = max
		}
	}
}

// initHeap is called when the heap is first initialized at program start.
func initHeap() {
	// Heap is initialized by the external allocator
}

func setHeapEnd(newHeapEnd uintptr) {
	// Heap is in custom GC, so ignore it when called from wasm initialization.
}

// alloc tries to find free space on the heap to allocate memory,
// possibly doing a garbage collection cycle if needed. If no space
// is free, it panics. layout is currently not used.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	printstr("call alloc(")
	printnum(int(size))
	printstr(")\n")

	if size == 0 {
		printstr("zero-size allocation\n")
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	if gcRunning {
		printstr("aborting, called alloc during GC cycle\n")
		abort()
	}

	size = align(size)
	gcMallocs++

	var gcRan bool
	for {
		// Try to bound heap growth.
		if gcTotalAlloc+uint64(size) < gcTotalAlloc {
			printstr("total memory")
			printnum(int(gcTotalAlloc))
			printstr("\n")
			printstr("target heap size exceeds address space\n")
			abort()
		}

		if gcTotalAlloc+uint64(size) > uint64(heapBound) {
			if !gcRan {
				printstr("reached the heap size limit\n")
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
				printstr("increased the heap size limit to ")
				printnum(int(heapBound))
				printstr("\n")
			}
		}

		// Ensure that there is space in the alloc list.
		if len(allocList) == cap(allocList) {
			printstr("alloc list is full\n")

			// Attempt to double the size of the alloc list.
			newCap := 2 * uintptr(cap(allocList))
			if newCap == 0 {
				newCap = 1
			}

			printstr("increase the capacity from ")
			printnum(cap(allocList))
			printstr(" to ")
			printnum(int(newCap))
			printstr("\n")

			// oldList := allocList

			oldListHeader := (*struct {
				ptr unsafe.Pointer
				len uintptr
				cap uintptr
			})(unsafe.Pointer(&allocList))

			printstr("old list\n")
			printstr("\tstart ")
			printnum(int(uintptr(oldListHeader.ptr)))
			printstr("\tend ")
			printnum(int(uintptr(oldListHeader.ptr) + oldListHeader.cap*unsafe.Sizeof(allocListEntry{})))
			printstr("\n")

			printstr("try to allocate memory for the new alloc list with size ")
			printnum(int(newCap * unsafe.Sizeof(allocListEntry{})))
			printstr("\n")

			newListPtr := extalloc(newCap * unsafe.Sizeof(allocListEntry{}))
			if newListPtr == nil {
				printstr("call to extalloc failed")

				if gcRan {
					// Garbage collector was not able to free up enough memory.
					printstr("out of memory\n")
					abort()
				} else {
					// Run the garbage collector and try again.
					GC()
					gcRan = true
					continue
				}
			}

			newListHeader := (*struct {
				ptr unsafe.Pointer
				len uintptr
				cap uintptr
			})(unsafe.Pointer(&allocList))
			newListHeader.ptr = newListPtr
			newListHeader.len = oldListHeader.len // uintptr(len(oldList))
			newListHeader.cap = newCap

			// copy(allocList, oldList)
			// for i := range oldList {
			// 	*(*allocListEntry)(unsafe.Pointer(uintptr(newListHeader.ptr) + uintptr(i)*unsafe.Sizeof(allocListEntry{}))) = oldList[i]
			// }
			for i := 0; i < int(oldListHeader.len); i++ {
				*(*allocListEntry)(unsafe.Pointer(uintptr(newListHeader.ptr) + uintptr(i)*unsafe.Sizeof(allocListEntry{}))) = *(*allocListEntry)(unsafe.Pointer(uintptr(oldListHeader.ptr) + uintptr(i)*unsafe.Sizeof(allocListEntry{})))
			}

			printstr("new list\n")
			printstr("\tstart ")
			printnum(int(uintptr(newListHeader.ptr)))
			printstr("\tend ")
			printnum(int(uintptr(newListHeader.ptr) + newListHeader.cap*unsafe.Sizeof(allocListEntry{})))
			printstr("\n")

			if oldListHeader.cap != 0 { // cap(oldList)
				printstr("free the old alloc list\n")
				free(oldListHeader.ptr) // unsafe.Pointer(&oldList[0])
			}
		}

		// Allocate the memory.
		pointer := extalloc(size)
		if pointer == nil {
			printstr("\textalloc call failed\n")

			if gcRan {
				// Garbage collector was not able to free up enough memory.
				printstr("out of memory\n")
				abort()
			} else {
				// Run the garbage collector and try again.
				GC()
				gcRan = true
				continue
			}
		}

		// Add the allocation to the list.
		i := len(allocList)
		// allocList = allocList[:i+1]
		// allocList[i] = allocListEntry{
		// 	start: uintptr(pointer),
		// 	end:   uintptr(pointer) + size,
		// }

		newListHeader := (*struct {
			ptr unsafe.Pointer
			len uintptr
			cap uintptr
		})(unsafe.Pointer(&allocList))

		*(*allocListEntry)(unsafe.Pointer(uintptr(newListHeader.ptr) + uintptr(i)*unsafe.Sizeof(allocListEntry{}))) = allocListEntry{
			start: uintptr(pointer),
			end:   uintptr(pointer) + size,
		}

		newListHeader.len = uintptr(i + 1)

		// printstr("updated new alloc list\n")
		// printstr("\tlength ")
		// printnum(len(allocList))
		// printstr("\n")
		// printstr("\tcapacity ")
		// printnum(cap(allocList))
		// printstr("\n")

		// Zero-out the allocated memory
		memzero(pointer, size)

		// Update used memory
		gcTotalAlloc += uint64(size)

		printstr("total memory ")
		printnum(int(gcTotalAlloc))
		printstr("\n")

		return pointer
	}
}

// free is called to explicitly free a previously allocated pointer.
func free(ptr unsafe.Pointer) {
	printstr("call free(")
	printnum(int(uintptr(ptr)))
	printstr(")\n")
	gcFrees++
	extfree(ptr)
}

// markRoots is called with the start and end addresses to scan for references.
// It is currently only called with the top and bottom of the stack.
func markRoots(start, end uintptr) {
	scan(start, end)
}

func markRoot(addr uintptr, root uintptr) {
	mark(root)
}

// GC is called to explicitly run garbage collection.
func GC() {
	// printstr("call GC()\n")

	// if gcRunning {
	// 	printstr("aborting recursive GC() call\n")
	// 	abort()
	// }
	// gcRunning = true

	// printstr("non-sorted pre-GC allocations\n")
	// for _, v := range allocList {
	// 	printstr("\t[")
	// 	printnum(int(v.start))
	// 	printstr(",")
	// 	printnum(int(v.end))
	// 	printstr("]\n")
	// }
	// printstr("\n")

	// // Sort the allocation list so that it can be efficiently searched.
	// sortAllocList(allocList)

	// // Unmark all allocations.
	// for i := range allocList {
	// 	allocList[i].next = nil
	// }

	// // Reset the scan queue.
	// scanQueue = nil

	// printstr("pre-GC allocations\n")
	// for _, v := range allocList {
	// 	printstr("\t[")
	// 	printnum(int(v.start))
	// 	printstr(",")
	// 	printnum(int(v.end))
	// 	printstr("]\n")
	// }
	// printstr("\n")

	// if len(allocList) > 1 {
	// 	for i, _ := range allocList[1:] {
	// 		if allocList[i+1].start < allocList[i].start { // <= ?
	// 			printstr("alloc list is not sorted\n")
	// 			abort()
	// 		}
	// 	}
	// }

	// // Start by scanning the stack.
	// // markStack()

	// // Scan all globals.
	// // markGlobals()

	// // Channel operations in interrupts may move task pointers around while we are marking.
	// // Therefore we need to scan the runqueue seperately.
	// // 	var markedTaskQueue task.Queue
	// // runqueueScan:
	// // 	for !runqueue.Empty() {
	// // 		// Pop the next task off of the runqueue.
	// // 		t := runqueue.Pop()

	// // 		// Mark the task if it has not already been marked.
	// // 		markRoot(uintptr(unsafe.Pointer(&runqueue)), uintptr(unsafe.Pointer(t)))

	// // 		// Push the task onto our temporary queue.
	// // 		markedTaskQueue.Push(t)
	// // 	}

	// // Scan all referenced allocations.
	// // for scanQueue != nil {
	// // 	// Pop a marked allocation off of the scan queue.
	// // 	alloc := scanQueue
	// // 	next := alloc.next
	// // 	if next == alloc {
	// // 		// This is the last value on the queue.
	// // 		next = nil
	// // 	}
	// // 	scanQueue = next

	// // 	// Scan and mark all allocations that this references.
	// // 	alloc.scan()
	// // }

	// // i := interrupt.Disable()
	// // if !runqueue.Empty() {
	// // 	// Something new came in while finishing the mark.
	// // 	interrupt.Restore(i)
	// // 	goto runqueueScan
	// // }
	// // runqueue = markedTaskQueue
	// // interrupt.Restore(i)

	// // Free all remaining unmarked allocations.
	// // gcTotalAlloc = 0
	// // j := 0
	// // for _, v := range allocList {
	// // 	if v.next == nil {
	// // 		// This was never marked.
	// // 		free(unsafe.Pointer(v.start))
	// // 		continue
	// // 	}

	// // 	// Move this down in the list.
	// // 	allocList[j] = v
	// // 	j++

	// // 	// Re-calculate used memory.
	// // 	gcTotalAlloc += uint64(v.end - v.start)
	// // }
	// // allocList = allocList[:j]

	// printstr("post-GC allocations\n")
	// for _, v := range allocList {
	// 	printstr("\t[")
	// 	printnum(int(v.start))
	// 	printstr(",")
	// 	printnum(int(v.end))
	// 	printstr("]\n")
	// }
	// printstr("\n")

	// gcRunning = false
}

// SetFinalizer registers a finalizer.
func SetFinalizer(obj interface{}, finalizer interface{}) {
	// TODO
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
