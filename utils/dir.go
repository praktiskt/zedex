package utils

import "os"

func CreateDirIfNotExists(dir string) {
	if dir == "" {
		return
	}
	os.MkdirAll(dir, os.ModePerm)
}
