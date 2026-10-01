package httpx

import "net/http"

// RecoverableResponse buffers ordinary responses so a panic cannot expose a
// partial success. An explicit flush switches to streaming; after that point
// recovery must abort the response instead of appending a JSON error to it.
type RecoverableResponse struct {
	buffer    *ResponseBuffer
	writer    http.ResponseWriter
	committed bool
}

func NewRecoverableResponse(writer http.ResponseWriter) *RecoverableResponse {
	return &RecoverableResponse{buffer: NewResponseBuffer(), writer: writer}
}
func (r *RecoverableResponse) Header() http.Header {
	if r.committed {
		return r.writer.Header()
	}
	return r.buffer.Header()
}
func (r *RecoverableResponse) WriteHeader(status int) {
	if r.committed {
		r.writer.WriteHeader(status)
	} else {
		r.buffer.WriteHeader(status)
	}
}
func (r *RecoverableResponse) Write(value []byte) (int, error) {
	if r.committed {
		return r.writer.Write(value)
	}
	return r.buffer.Write(value)
}
func (r *RecoverableResponse) Unwrap() http.ResponseWriter { return r.writer }
func (r *RecoverableResponse) Committed() bool             { return r.committed }
func (r *RecoverableResponse) FlushError() error {
	r.Finish()
	return http.NewResponseController(r.writer).Flush()
}
func (r *RecoverableResponse) Finish() {
	if !r.committed {
		r.committed = true
		r.buffer.FlushTo(r.writer)
	}
}
