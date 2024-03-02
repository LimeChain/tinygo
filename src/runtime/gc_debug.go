//go:build gc.extalloc || gc.extalloc_leaking

package runtime

const gcDebug = true

func printnum(num int) {
	if num == 0 {
		printstr("0")
		return
	}

	digits := [16]int{} // store up to 16 digits
	digitStrings := [10]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	count := 0 // count of digits

	// extract digits from the number
	for ; num > 0; num /= 10 {
		digits[count] = num % 10
		count++
	}

	// reverse the digits
	for i := 0; i < count/2; i++ {
		j := count - i - 1
		digits[i], digits[j] = digits[j], digits[i]
	}

	// print each digit
	for i := 0; i < count; i++ {
		printstr(digitStrings[digits[i]])
	}
}

func printstr(str string) {
	if !gcDebug {
		return
	}

	for i := 0; i < len(str); i++ {
		if putcharPosition >= putcharBufferSize {
			break
		}

		putcharBuffer[putcharPosition] = str[i]
		putcharPosition++
	}
}

//go:export _write_debug_info
func writeDebugInfo(int32, int32) int64 {
	printallocs()
	return 0
}

func printalloc(curAlloc heapAllocation, i int) int {
	size := int(curAlloc.end - curAlloc.start)
	// printstr("alloc[")
	// printnum(i)
	// printstr("]: size ")
	// printnum(size)
	// printstr(" at ")
	// printnum(int(curAlloc.start))
	// printstr(" - ")
	// printnum(int(curAlloc.end))
	// printstr("\n")

	return size
}

func printallocs() {
	var totalSize int
	for i, curAlloc := range allocations {
		totalSize += printalloc(curAlloc, i)
	}

	printstr("total heap allocations - count: ")
	printnum(len(allocations))
	printstr(" size: ")
	printnum(int(totalSize))
	printstr("\n")
	// reset
	putcharPosition = 0
}
