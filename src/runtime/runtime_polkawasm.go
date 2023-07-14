//go:build polkawasm

package runtime

import "unsafe"

// //export _start
// func _start() {
// 	// These need to be initialized early so that the heap can be initialized.
// 	heapStart = uintptr(unsafe.Pointer(&heapStartSymbol))
// 	heapEnd = uintptr(wasm_memory_size(0) * wasmPageSize)
// 	run()
// }

// Using global variables to avoid heap allocation.
const putcharBufferSize = 256 // increase the debug output size

var (
	putcharBuffer        = [putcharBufferSize]byte{}
	putcharPosition uint = 0
)

// //go:export _debug_buf
// func debugBuf() uintptr {
// 	return uintptr(unsafe.Pointer(&putcharBuffer[0]))
// }

// Abort executes the wasm 'unreachable' instruction.
func abort() {
	trap()
}

func putchar(c byte) {
	putcharBuffer[putcharPosition] = c
	putcharPosition++
}

func getchar() byte {
	return 0
}

func buffered() int {
	return 0
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
func now() (sec int64, nsec int32, mono int64) {
	panic("unimplemented: now")
}

//go:linkname syscall_runtime_envs syscall.runtime_envs
func syscall_runtime_envs() []string {
	panic("unimplemented: syscall_runtime_envs")
}

//go:linkname os_runtime_args os.runtime_args
func os_runtime_args() []string {
	return []string{}
}

//go:linkname syscall_Exit syscall.Exit
func syscall_Exit(code int) {
	return
}

//go:linkname procPin sync/atomic.runtime_procPin
func procPin() {

}

//go:linkname procUnpin sync/atomic.runtime_procUnpin
func procUnpin() {

}

//go:wasmimport env ext_allocator_malloc_version_1
func extalloc(size uintptr) unsafe.Pointer

//go:wasmimport env ext_allocator_free_version_1
func extfree(ptr unsafe.Pointer)
