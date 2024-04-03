package utils

import "encoding/binary"

func GetVersionBz(version uint16) []byte {
	res := make([]byte, 2)
	binary.BigEndian.PutUint16(res, version)
	return res
}

func GetCodeIDBz(codeID uint64) []byte {
	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, codeID)
	return res
}
