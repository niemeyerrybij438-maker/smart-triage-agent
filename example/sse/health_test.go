package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	healthHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload healthResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("status payload = %q, want ok", payload.Status)
	}
}

func TestReadinessHandlerWithoutDatabase(t *testing.T) {
	previousDB := globalDB
	globalDB = nil
	t.Cleanup(func() { globalDB = previousDB })

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	readinessHandler(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var payload healthResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Checks["database"] != "not_initialized" {
		t.Fatalf("database check = %q, want not_initialized", payload.Checks["database"])
	}
}

func TestReadinessHandlerWithDatabase(t *testing.T) {
	db, _ := newMockGormDB(t)
	useGlobalDBForTest(t, db)

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	readinessHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload healthResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "ready" || payload.Checks["database"] != "ok" {
		t.Fatalf("unexpected readiness payload: %+v", payload)
	}
}
