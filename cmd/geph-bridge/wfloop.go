package main

import (
	"net/http"

	"github.com/geph-official/geph2/libs/warpfront"
)

func wfLoop() {
	wfs := warpfront.NewServer()
	server := &http.Server{
		Addr:    wfAddr,
		Handler: wfs,
	}
	go func() {
		panic(server.ListenAndServe())
	}()
	for {
		client, err := wfs.Accept()
		if err != nil {
			panic(err)
		}
		go handle(client)
	}
}
