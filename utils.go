package websocket

import (
	"reflect"
	"unsafe"
)

func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func s2b(s string) []byte {
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: sh.Data,
		Len:  sh.Len,
		Cap:  sh.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}

func equalsFold(b, s []byte) (equals bool) {
	n := len(b)

	if equals = n == len(s); equals {
		for i := 0; i < n; i++ {
			if equals = b[i]|0x20 == s[i]|0x20; !equals {
				break
			}
		}
	}

	return
}
