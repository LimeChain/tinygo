//go:build gc.custom || gc.custom_wip

package runtime

const gcDebug = false

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
