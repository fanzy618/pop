package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/fanzy618/pop/internal/proxy"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "proxy listen address")
	flag.Parse()

	srv := &http.Server{
		Addr:    *listen,
		Handler: proxy.NewServer(),
	}

	log.Printf("pop proxy listening on %s", *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server stopped: %v", err)
	}
}
