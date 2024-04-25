package crossover_cache

import (
	"bytes"
	"net/http"
)

type responseRecorder struct {
	rw     http.ResponseWriter
	status int
	body   bytes.Buffer
	header http.Header
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b) // Just buffer the body, don't write to rw

}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
}
