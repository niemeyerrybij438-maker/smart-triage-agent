package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status    string            `json:"status"`
	Checks    map[string]string `json:"checks,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeHealthResponse(w, http.StatusMethodNotAllowed, healthResponse{Status: "method_not_allowed", Timestamp: time.Now().UTC()})
		return
	}
	writeHealthResponse(w, http.StatusOK, healthResponse{Status: "ok", Timestamp: time.Now().UTC()})
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeHealthResponse(w, http.StatusMethodNotAllowed, healthResponse{Status: "method_not_allowed", Timestamp: time.Now().UTC()})
		return
	}

	response := healthResponse{
		Status:    "ready",
		Checks:    map[string]string{"database": "ok"},
		Timestamp: time.Now().UTC(),
	}
	if globalDB == nil {
		response.Status = "not_ready"
		response.Checks["database"] = "not_initialized"
		writeHealthResponse(w, http.StatusServiceUnavailable, response)
		return
	}

	sqlDB, err := globalDB.DB()
	if err != nil {
		response.Status = "not_ready"
		response.Checks["database"] = "unavailable"
		writeHealthResponse(w, http.StatusServiceUnavailable, response)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		response.Status = "not_ready"
		response.Checks["database"] = "unavailable"
		writeHealthResponse(w, http.StatusServiceUnavailable, response)
		return
	}

	writeHealthResponse(w, http.StatusOK, response)
}

func writeHealthResponse(w http.ResponseWriter, status int, response healthResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
