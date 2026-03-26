// Package server wires up the HTTP server with timeouts and request logging.
package server

import (
	"log"
	"net/http"
	"time"
)

// New returns a configured *http.Server. The caller is responsible for calling
// ListenAndServe on the returned server.
func New(addr string, mux http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      logging(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
