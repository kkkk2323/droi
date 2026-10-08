package main

import (
	"log"
	"net/http"
	"net/http/pprof"
	"os"
)

// servePprof serves Go's profiles on DROI_PPROF (such as 127.0.0.1:6061)
// when it is set, to see what holds the app's memory; it does nothing
// otherwise.
func servePprof() {
	addr := os.Getenv("DROI_PPROF")
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("droi: pprof on %s: %v", addr, err)
		}
	}()
}
