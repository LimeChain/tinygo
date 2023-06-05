//go:build polkawasm
// +build polkawasm

package runtime

import (
	"unsafe"
)

// no need to export "_start", since it is the host allocator's
// responsibility to initialize and grow the heap as needed
func _start() {
	// These need to be initialized early so that the heap can be initialized.
	heapStart = uintptr(unsafe.Pointer(&heapStartSymbol))
	heapEnd = uintptr(wasm_memory_size(0) * wasmPageSize)

	// run()
	initHeap()
	// initAll()
	callMain()
}

// Using global variables to avoid heap allocation.
const putcharBufferSize = 256 // increase the debug output size

var (
	putcharBuffer        = [putcharBufferSize]byte{}
	putcharPosition uint = 0
)

// export as "_debug_buf" to read the debug output from the host
func debugBuf() uintptr {
	return uintptr(unsafe.Pointer(&putcharBuffer[0]))
}

func putchar(c byte) {
	putcharBuffer[putcharPosition] = c
	putcharPosition++
}

func getchar() byte {
	// TODO
	return 0
}

func buffered() int {
	// TODO
	return 0
}

// Abort executes the wasm 'unreachable' instruction.
func abort() {
	trap()
}

//go:linkname os_runtime_args os.runtime_args
func os_runtime_args() []string {
	return []string{}
}

//go:linkname syscall_runtime_envs syscall.runtime_envs
func syscall_runtime_envs() []string {
	return []string{}
}

type timeUnit int64

func ticksToNanoseconds(ticks timeUnit) int64 {
	panic("unimplemented: ticksToNanoseconds")
}

func nanosecondsToTicks(ns int64) timeUnit {
	panic("unimplemented: nanosecondsToTicks")
}

func sleepTicks(d timeUnit) {
	panic("unimplemented: sleepTicks")
}

func ticks() timeUnit {
	panic("unimplemented: ticks")
}

//go:linkname now time.now
func now() (int64, int32, int64) {
	panic("unimplemented: now")
}

//go:linkname syscall_Exit syscall.Exit
func syscall_Exit(code int) {
	return
}

// TinyGo does not yet support any form of parallelism on WebAssembly, so these
// can be left empty.

//go:linkname procPin sync/atomic.runtime_procPin
func procPin() {
}

//go:linkname procUnpin sync/atomic.runtime_procUnpin
func procUnpin() {
}

//go:wasm-module env
//go:export ext_allocator_malloc_version_1
func extalloc(size uintptr) unsafe.Pointer

//go:wasm-module env
//go:export ext_allocator_free_version_1
func extfree(ptr unsafe.Pointer)
