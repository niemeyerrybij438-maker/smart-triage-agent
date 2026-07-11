package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIndexHandlerServesEmbeddedPatientPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	indexHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != len(patientPage) {
		t.Fatalf("body length = %d, want %d", response.Body.Len(), len(patientPage))
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
	}
}

func TestDoctorLoginPageHandlerServesEmbeddedPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/doctor/login", nil)
	response := httptest.NewRecorder()

	doctorLoginPageHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != len(doctorLoginPage) {
		t.Fatalf("body length = %d, want %d", response.Body.Len(), len(doctorLoginPage))
	}
}

func TestDoctorHandlerRequiresLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/doctor", nil)
	response := httptest.NewRecorder()

	doctorHandler(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/doctor/login" {
		t.Fatalf("redirect location = %q, want %q", location, "/doctor/login")
	}
}

func TestDoctorHandlerServesEmbeddedPageAfterLogin(t *testing.T) {
	token := "test-doctor-session"
	doctorSessionsMu.Lock()
	doctorSessions[token] = doctorSession{Username: "doctor", DisplayName: "Doctor", ExpiresAt: time.Now().Add(time.Hour)}
	doctorSessionsMu.Unlock()
	t.Cleanup(func() {
		doctorSessionsMu.Lock()
		delete(doctorSessions, token)
		doctorSessionsMu.Unlock()
	})

	request := httptest.NewRequest(http.MethodGet, "/doctor", nil)
	request.AddCookie(&http.Cookie{Name: doctorSessionCookie, Value: token})
	response := httptest.NewRecorder()

	doctorHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != len(doctorPage) {
		t.Fatalf("body length = %d, want %d", response.Body.Len(), len(doctorPage))
	}
}
