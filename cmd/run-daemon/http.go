package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/taybart/run"
)

func (s *Server) handleList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dir := q.Get("dir")
		if dir == "" {
			http.Error(w, "missing dir", http.StatusBadRequest)
			return
		}
		dir = filepath.Clean(dir)
		prefix := dir
		if !strings.HasSuffix(prefix, string(filepath.Separator)) { // "/" already ends with one
			prefix += string(filepath.Separator)
		}

		res := s.targets.list(func(k string) bool {
			return k == dir || strings.HasPrefix(k, prefix)
		})

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
		results, err := s.targets.restart(req)
		if errors.Is(err, errNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// one result per instance, 500 if any didn't come back up
		w.Header().Set("Content-Type", "application/json")
		if !allOK(results) {
			w.WriteHeader(http.StatusInternalServerError)
		}
		if err := json.NewEncoder(w).Encode(results); err != nil {
			fmt.Println(err)
		}
	}
}

func (s *Server) handleLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		req := run.Process{
			Dir:    q.Get("dir"),
			Script: q.Get("script"),
			Args:   q["args"],
		}
		if req.Dir == "" || req.Script == "" {
			http.Error(w, "missing dir or script", http.StatusBadRequest)
			return
		}
		results, err := s.targets.logs(req)
		if errors.Is(err, errNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			fmt.Println(err)
		}
	}
}

func allOK(results []run.ResetResult) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

func (s *Server) listenHTTP() error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /list", s.handleList())
	mux.HandleFunc("GET /logs", s.handleLogs())
	mux.HandleFunc("POST /restart", s.handleRestart())
	mux.HandleFunc("POST /mcp", s.handleMCP())

	srv := &http.Server{
		// only listen on localhost since this exposes running logs
		Addr:              "localhost:19230",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("listening for requests on", srv.Addr)
	return srv.ListenAndServe()
}
