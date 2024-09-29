package slices

import (
	"fmt"
)

func Split[T any](slice []T, size int) [][]T {
	if size <= 0 {
		panic(fmt.Sprintf("could not split a slice, %d <= 0", size))
	}

	var chunks [][]T
	for size < len(slice) {
		slice, chunks = slice[size:], append(chunks, slice[0:size])
	}

	return append(chunks, slice)
}

func Convert[A any, B any](slice []A, fn func(A, int) B) []B {
	var result = make([]B, len(slice))
	for i := range slice {
		result[i] = fn(slice[i], i)
	}

	return result
}
