package utils

func IfElse[T any](c bool, vT, vF T) T {
	if c {
		return vT
	}
	return vF
}
