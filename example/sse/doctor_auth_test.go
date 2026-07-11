package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoctorConfigurationDefaults(t *testing.T) {
	t.Setenv("DOCTOR_USERNAME", "")
	t.Setenv("DOCTOR_PASSWORD", "")
	t.Setenv("DOCTOR_DISPLAY_NAME", "")

	username, password := doctorCredentials()
	if username != "doctor" || password != "doctor123" {
		t.Fatalf("credentials = %q/%q, want default credentials", username, password)
	}
	if displayName := doctorDisplayName(); displayName != "\u674e\u533b\u751f" {
		t.Fatalf("display name = %q, want default", displayName)
	}
}

func TestDoctorConfigurationFromEnvironment(t *testing.T) {
	t.Setenv("DOCTOR_USERNAME", " clinician ")
	t.Setenv("DOCTOR_PASSWORD", "secret")
	t.Setenv("DOCTOR_DISPLAY_NAME", " \u738b\u533b\u751f ")

	username, password := doctorCredentials()
	if username != "clinician" || password != "secret" {
		t.Fatalf("credentials = %q/%q, want configured credentials", username, password)
	}
	if displayName := doctorDisplayName(); displayName != "\u738b\u533b\u751f" {
		t.Fatalf("display name = %q, want configured display name", displayName)
	}
}

func TestSecureEqual(t *testing.T) {
	if !secureEqual("same", "same") {
		t.Fatal("equal values must match")
	}
	if secureEqual("same", "different") {
		t.Fatal("different values must not match")
	}
}

func TestExpiredDoctorSessionIsRejectedAndRemoved(t *testing.T) {
	token := "expired-test-session"
	doctorSessionsMu.Lock()
	doctorSessions[token] = doctorSession{Username: "doctor", ExpiresAt: time.Now().Add(-time.Minute)}
	doctorSessionsMu.Unlock()

	request := httptest.NewRequest(http.MethodGet, "/doctor", nil)
	request.AddCookie(&http.Cookie{Name: doctorSessionCookie, Value: token})

	if _, ok := currentDoctorSession(request); ok {
		t.Fatal("expired session must be rejected")
	}
	doctorSessionsMu.Lock()
	_, exists := doctorSessions[token]
	doctorSessionsMu.Unlock()
	if exists {
		t.Fatal("expired session must be removed")
	}
}
