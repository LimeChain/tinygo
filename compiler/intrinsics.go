package compiler

// This file contains helper functions to create calls to LLVM intrinsics.

import (
	"go/token"
	"strings"

	"tinygo.org/x/go-llvm"
)

// Define unimplemented intrinsic functions.
//
// Some functions are either normally implemented in Go assembly (like
// sync/atomic functions) or intentionally left undefined to be implemented
// directly in the compiler (like runtime/volatile functions). Either way, look
// for these and implement them if this is the case.
func (b *builder) defineIntrinsicFunction() {
	name := b.fn.RelString(nil)
	switch {
	case strings.HasPrefix(name, "runtime/volatile.Load"):
		b.createVolatileLoad()
	case strings.HasPrefix(name, "runtime/volatile.Store"):
		b.createVolatileStore()
	case strings.HasPrefix(name, "sync/atomic.") && token.IsExported(b.fn.Name()):
		b.createFunctionStart(true)
		returnValue := b.createAtomicOp(b.fn.Name())
		if !returnValue.IsNil() {
			b.CreateRet(returnValue)
		} else {
			b.CreateRetVoid()
		}
	}
}

var mathToLLVMMapping = map[string]string{
	"math.Ceil":  "llvm.ceil.f64",
	"math.Exp":   "llvm.exp.f64",
	"math.Exp2":  "llvm.exp2.f64",
	"math.Floor": "llvm.floor.f64",
	"math.Log":   "llvm.log.f64",
	"math.Sqrt":  "llvm.sqrt.f64",
	"math.Trunc": "llvm.trunc.f64",
}

// defineMathOp defines a math function body as a call to a LLVM intrinsic,
// instead of the regular Go implementation. This allows LLVM to reason about
// the math operation and (depending on the architecture) allows it to lower the
// operation to very fast floating point instructions. If this is not possible,
// LLVM will emit a call to a libm function that implements the same operation.
//
// One example of an optimization that LLVM can do is to convert
// float32(math.Sqrt(float64(v))) to a 32-bit floating point operation, which is
// beneficial on architectures where 64-bit floating point operations are (much)
// more expensive than 32-bit ones.
func (b *builder) defineMathOp() {
	b.createFunctionStart(true)
	llvmName := mathToLLVMMapping[b.fn.RelString(nil)]
	if llvmName == "" {
		panic("unreachable: unknown math operation") // sanity check
	}
	llvmFn := b.mod.NamedFunction(llvmName)
	if llvmFn.IsNil() {
		// The intrinsic doesn't exist yet, so declare it.
		// At the moment, all supported intrinsics have the form "double
		// foo(double %x)" so we can hardcode the signature here.
		llvmType := llvm.FunctionType(b.ctx.DoubleType(), []llvm.Type{b.ctx.DoubleType()}, false)
		llvmFn = llvm.AddFunction(b.mod, llvmName, llvmType)
	}
	// Create a call to the intrinsic.
	args := make([]llvm.Value, len(b.fn.Params))
	for i, param := range b.fn.Params {
		args[i] = b.getValue(param)
	}
	result := b.CreateCall(llvmFn.GlobalValueType(), llvmFn, args, "")
	b.CreateRet(result)
}
