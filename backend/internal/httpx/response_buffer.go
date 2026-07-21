package httpx

import (
	"bytes"
	"net/http"
)

type ResponseBuffer struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func NewResponseBuffer() *ResponseBuffer {
	return &ResponseBuffer{header: make(http.Header)}
}

func (b *ResponseBuffer) Header() http.Header {
	return b.header
}

func (b *ResponseBuffer) WriteHeader(status int) {
	if b.status != 0 {
		return
	}
	b.status = status
}

func (b *ResponseBuffer) Write(value []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(value)
}

func (b *ResponseBuffer) Status() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (b *ResponseBuffer) FlushTo(w http.ResponseWriter) {
	for name, values := range b.header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(b.Status())
	_, _ = w.Write(b.body.Bytes())
}
