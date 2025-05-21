package utils

import (
	"crypto/md5"
	"encoding/binary"
)

func StringToUInt64Hash(s string) uint64 {
	hash := md5.New()
	if _, err := hash.Write([]byte(s)); err != nil {
		return 0
	}
	hashSum := hash.Sum(nil)
	hashBytes := hashSum[:8]
	return binary.BigEndian.Uint64(hashBytes)
}

func StringToUin32Hash(s string) uint32 {
	return binary.BigEndian.Uint32([]byte(s))
}
