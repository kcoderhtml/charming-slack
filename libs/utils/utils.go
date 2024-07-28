package utils

func ClampString(s string, max int) string {
	// convert the string to a slice of runes
	runes := []rune(s)

	// if the length of the string is greater than the max, return the first max runes
	if len(runes) > max {
		return string(runes[:max])
	}

	// if the length of the string is less than the max, return the string
	return s
}
