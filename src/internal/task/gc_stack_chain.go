//go:build (gc.conservative || gc.extallocleak) && tinygo.wasm
// +build gc.conservative gc.extallocleak
// +build tinygo.wasm

package task

import "unsafe"

//go:linkname swapStackChain runtime.swapStackChain
func swapStackChain(dst *unsafe.Pointer)

type gcData struct {
	stackChain unsafe.Pointer
}

func (gcd *gcData) swap() {
	swapStackChain(&gcd.stackChain)
}
