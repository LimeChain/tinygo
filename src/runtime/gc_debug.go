//go:build gc.extalloc || gc.extalloc_leaking

package runtime

const gcDebug = false

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

		putchar(str[i])
	}
}
