// //go:build gc.extallocleak
// // +build gc.extallocleak

package runtime

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

// usedMem is the total amount of allocated memory
var usedMem uintptr

// zeroSizedAlloc is just a sentinel that gets returned when allocating 0 bytes.
var zeroSizedAlloc uint8

// alloc tries to find some free space on the heap, possibly doing a garbage
// collection cycle if needed. If no space is free, it panics.
//
//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	printstr("alloc(")
	printnum(int(size))
	printstr(")\n")

	if size == 0 {
		return unsafe.Pointer(&zeroSizedAlloc)
	}

	printstr("\tused memory ")
	printnum(int(usedMem))
	printstr("\n")

	// Try to bound heap growth.
	if usedMem+size < usedMem {
		printstr("\tout of memory\n")
		abort()
	}

	// Allocate the memory.
	pointer := extalloc(size)
	if pointer == nil {
		printstr("\textalloc call failed\n")
		abort()
	}

	memzero(pointer, size)
	usedMem += size
	return pointer
}

func free(ptr unsafe.Pointer) {
	// memory is never explicitly freed
	extfree(ptr)
}

func GC() {
	// Unimplemented.
}

func initHeap() {
	// Nothing to initialize.
}

func setHeapEnd(newHeapEnd uintptr) {
	// Nothing to do here, this function is never actually called.
}

func markRoots(start, end uintptr) {
	// dummy, so that markGlobals will compile
}

func markRoot(addr, root uintptr) {
	// dummy
}

func ReadMemStats(m *MemStats) {
	// Unimplemented.
}
