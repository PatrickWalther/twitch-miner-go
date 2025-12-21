package web

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONOK writes a JSON response with 200 OK status
func writeJSONOK(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, v)
}

// writeSuccess writes a simple {"status": "ok"} response
func writeSuccess(w http.ResponseWriter) {
	writeJSONOK(w, map[string]string{"status": "ok"})
}

// writeError writes an HTTP error response
func writeError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// writeInternalError writes a 500 Internal Server Error
func writeInternalError(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusInternalServerError, msg)
}

// writeBadRequest writes a 400 Bad Request error
func writeBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, msg)
}

// writeNotAllowed writes a 405 Method Not Allowed error
func writeNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// writeServiceUnavailable writes a 503 Service Unavailable error
func writeServiceUnavailable(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusServiceUnavailable, msg)
}
