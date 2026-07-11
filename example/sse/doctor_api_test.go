package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func resetDoctorSessions(t *testing.T) {
	t.Helper()
	doctorSessionsMu.Lock()
	previous := doctorSessions
	doctorSessions = make(map[string]doctorSession)
	doctorSessionsMu.Unlock()
	t.Cleanup(func() {
		doctorSessionsMu.Lock()
		doctorSessions = previous
		doctorSessionsMu.Unlock()
	})
}

func loginDoctorForTest(t *testing.T, displayName string) *http.Cookie {
	t.Helper()
	t.Setenv("DOCTOR_USERNAME", "doctor")
	t.Setenv("DOCTOR_PASSWORD", "doctor123")
	t.Setenv("DOCTOR_DISPLAY_NAME", displayName)

	body := bytes.NewBufferString(`{"username":"doctor","password":"doctor123"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/doctor/login", body)
	response := httptest.NewRecorder()
	doctorLoginHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %q", response.Code, response.Body.String())
	}
	result := response.Result()
	for _, cookie := range result.Cookies() {
		if cookie.Name == doctorSessionCookie {
			return cookie
		}
	}
	t.Fatal("login response did not set doctor session cookie")
	return nil
}

func TestDoctorLoginRejectsInvalidCredentials(t *testing.T) {
	resetDoctorSessions(t)
	t.Setenv("DOCTOR_USERNAME", "doctor")
	t.Setenv("DOCTOR_PASSWORD", "doctor123")

	request := httptest.NewRequest(http.MethodPost, "/api/doctor/login", bytes.NewBufferString(`{"username":"doctor","password":"wrong"}`))
	response := httptest.NewRecorder()
	doctorLoginHandler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestDoctorLoginMeAndLogoutLifecycle(t *testing.T) {
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "\u674e\u533b\u751f")

	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 12*60*60 {
		t.Fatalf("unexpected login cookie: %+v", cookie)
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/api/doctor/me", nil)
	meRequest.AddCookie(cookie)
	meResponse := httptest.NewRecorder()
	doctorMeHandler(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %q", meResponse.Code, meResponse.Body.String())
	}
	var me map[string]string
	if err := json.Unmarshal(meResponse.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if me["username"] != "doctor" || me["displayName"] != "\u674e\u533b\u751f" {
		t.Fatalf("unexpected me response: %#v", me)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/doctor/logout", nil)
	logoutRequest.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	doctorLogoutHandler(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout status = %d", logoutResponse.Code)
	}

	meAfterLogout := httptest.NewRequest(http.MethodGet, "/api/doctor/me", nil)
	meAfterLogout.AddCookie(cookie)
	meAfterLogoutResponse := httptest.NewRecorder()
	doctorMeHandler(meAfterLogoutResponse, meAfterLogout)
	if meAfterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want %d", meAfterLogoutResponse.Code, http.StatusUnauthorized)
	}
}
