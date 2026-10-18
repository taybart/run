package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/taybart/run"
)

func (s *Server) handleList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dir := q.Get("dir")
		if dir == "" {
			w.WriteHeader(http.StatusBadRequest)
		}
		res := make([]run.Process, 0, len(s.targets[dir]))
		for _, t := range s.targets[dir] {
			res = append(res, run.Process{
				Script: t.script,
				Args:   t.args,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(res); err != nil {
			fmt.Println(err)
		}
	}
}

func (s *Server) handleRestart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req run.Process
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Dir == "" || req.Script == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Printf("restarting: dir(%s) $ %s %s\n", req.Dir, req.Script, strings.Join(req.Args, " "))
		// FIXME: this will break if there are two of the same thing (directory/script/args) running
		//        i don't know if i really know why i would ever need to do this but we will get there when we get there
		reqKey := makeKey(req)
		for k, t := range s.targets[req.Dir] {
			if k == reqKey {
				t.bus.Write(run.Message{Action: run.ActionReset})
				fmt.Fprintf(w, "restarted %s\n", t.script)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) listenHTTP() error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /list", s.handleList())
	mux.HandleFunc("POST /restart", s.handleRestart())
	mux.HandleFunc("POST /mcp", s.handleMCP())

	srv := &http.Server{
		Addr:              ":19230",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("listening for requests on", srv.Addr)
	return srv.ListenAndServe()
}
