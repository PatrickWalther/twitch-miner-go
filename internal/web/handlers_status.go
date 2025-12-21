package web

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte("Connected"))
}

func (s *Server) handleAPIMinerStatus(w http.ResponseWriter, r *http.Request) {
	status := s.status.GetStatus()
	writeJSONOK(w, status)
}

func (s *Server) handleAPIMinerStatusStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeInternalError(w, "SSE not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.status.Subscribe()
	defer s.status.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case status, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(status)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
