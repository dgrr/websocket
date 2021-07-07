package websocket

import (
	"crypto/sha1"
	b64 "encoding/base64"
	"github.com/valyala/fasthttp"
	"hash"
	"net/http"
	"sync"
)

type (
	// RequestHandler is the websocket connection handler.
	RequestHandler func(conn *Conn)
	// UpgradeHandler is a middleware callback that determines whether the
	// WebSocket connection should be upgraded or not. If UpgradeHandler returns false,
	// the connection is not upgraded.
	UpgradeHandler func(*fasthttp.RequestCtx) bool
	// UpgradeNetHandler is like UpgradeHandler but for net/http.
	UpgradeNetHandler func(resp http.ResponseWriter, req *http.Request) bool
)

func prepareOrigin(b []byte, uri *fasthttp.URI) []byte {
	b = append(b[:0], uri.Scheme()...)
	b = append(b, "://"...)
	return append(b, uri.Host()...)
}

var shaPool = sync.Pool{
	New: func() interface{} {
		return sha1.New()
	},
}

var base64 = b64.StdEncoding

func makeKey(dst, key []byte) []byte {
	h := shaPool.Get().(hash.Hash)
	h.Reset()
	defer shaPool.Put(h)

	h.Write(key)
	h.Write(uidKey)
	dst = h.Sum(dst[:0])
	dst = appendEncode(base64, dst, dst)
	return dst
}

// Thank you @valyala
//
// https://go-review.googlesource.com/c/go/+/37639
func appendEncode(enc *b64.Encoding, dst, src []byte) []byte {
	n := enc.EncodedLen(len(src)) + len(dst)
	b := extendByteSlice(dst, n)
	n = len(dst)
	enc.Encode(b[n:], src)
	return b[n:]
}

func appendDecode(enc *b64.Encoding, dst, src []byte) ([]byte, error) {
	needLen := enc.DecodedLen(len(src)) + len(dst)
	b := extendByteSlice(dst, needLen)
	n, err := enc.Decode(b[len(dst):], src)
	return b[:len(dst)+n], err
}

func selectProtocol(protos [][]byte, accepted []string) string {
	if len(protos) == 0 {
		return ""
	}

	for _, proto := range protos {
		for _, accept := range accepted {
			if b2s(proto) == accept {
				return accept
			}
		}
	}
	return string(protos[0])
}
