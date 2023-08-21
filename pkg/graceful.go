package pkg

import (
	"crypto/rand"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/ory/graceful"
)

type GracefulConfig struct {
	Handler *http.ServeMux
	Port    string
}

func Graceful(Handler func() *GracefulConfig) error {

	h := Handler()

	server := http.Server{
		Handler:      h.Handler,
		Addr:         ":" + h.Port,
		ReadTimeout:  time.Duration(time.Second) * 30,
		WriteTimeout: time.Duration(time.Second) * 60,
		IdleTimeout:  time.Duration(time.Second) * 60,
		TLSConfig: &tls.Config{
			Rand:               rand.Reader,
			InsecureSkipVerify: false,
		},
	}

	return graceful.Graceful(server.ListenAndServe, server.Shutdown)
}
