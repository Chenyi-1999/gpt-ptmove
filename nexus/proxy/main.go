package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	policy, err := loadMediaPolicy(os.Getenv("PTMOVE_PROXY_TOKEN"), os.Getenv("PTMOVE_MEDIA_ROOTS"))
	if err != nil {
		log.Fatal(err)
	}
	address := os.Getenv("PTMOVE_LISTEN")
	if address == "" {
		address = "127.0.0.1:19090"
	}
	server := &http.Server{Addr: address, Handler: newMediaHandler(policy), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 25 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	log.Printf("PTMove media agent 0.3.1 listening on %s", address)
	log.Fatal(server.ListenAndServe())
}
