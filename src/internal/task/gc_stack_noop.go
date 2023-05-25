//go:build (!gc.conservative && !gc.extallocleak) || !tinygo.wasm
// +build !gc.conservative,!gc.extallocleak !tinygo.wasm

package task

type gcData struct{}

func (gcd *gcData) swap() {
}
