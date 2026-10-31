package websocket

import (
	"bufio"
	"net"
	"net/http"
)

// handshakeWriter keeps coder's validation authoritative while translating its
// pre-upgrade HTTP errors into the application's JSON contract. It also retains
// the underlying socket so shutdown can interrupt an in-progress close handshake.
type handshakeWriter struct {
	http.ResponseWriter
	network  net.Conn
	rejected bool
}

func (w *handshakeWriter) WriteHeader(status int) {
	if status < 400 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.rejected {
		return
	}
	w.rejected = true
	code := "VALIDATION_ERROR"
	switch {
	case status >= 500:
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
	case status == http.StatusForbidden:
		code = "FORBIDDEN"
	default:
		status = http.StatusBadRequest
	}
	httpError(w.ResponseWriter, status, code)
}
func (w *handshakeWriter) Write(b []byte) (int, error) {
	if w.rejected {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
func (w *handshakeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.network = conn
	}
	return conn, buffer, err
}
