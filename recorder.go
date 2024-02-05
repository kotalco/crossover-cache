package crossover_cache

import (
	"bytes"
	"net/http"
)

type responseRecorder struct {
	rw     http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *responseRecorder) Header() http.Header {
	return r.rw.Header()
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.rw.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.rw.WriteHeader(statusCode)
}
