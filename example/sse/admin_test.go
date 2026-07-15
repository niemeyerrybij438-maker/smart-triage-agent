package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func resetAdminSessions(t *testing.T) {
	t.Helper()
	adminSessionsMu.Lock()
	previous := adminSessions
	adminSessions = make(map[string]adminSession)
	adminSessionsMu.Unlock()
	t.Cleanup(func() {
		adminSessionsMu.Lock()
		adminSessions = previous
		adminSessionsMu.Unlock()
	})
}

func adminCookieForTest(t *testing.T) *http.Cookie {
	t.Helper()
	token := "admin-test-session"
	adminSessionsMu.Lock()
	adminSessions[token] = adminSession{Username: "admin", DisplayName: "Administrator", ExpiresAt: time.Now().Add(time.Hour)}
	adminSessionsMu.Unlock()
	return &http.Cookie{Name: adminSessionCookie, Value: token}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := hashPassword("secret123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if hash == "secret123" || !verifyPassword(hash, "secret123") {
		t.Fatal("password hash must verify without storing plaintext")
	}
	if verifyPassword(hash, "wrong") {
		t.Fatal("wrong password must not verify")
	}
	if _, err := hashPassword("short"); err == nil {
		t.Fatal("short password must be rejected")
	}
}

func TestAdminPagesRequireAuthentication(t *testing.T) {
	resetAdminSessions(t)
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	response := httptest.NewRecorder()
	adminHandler(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unexpected admin redirect: status=%d location=%q", response.Code, response.Header().Get("Location"))
	}

	loginRequest := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	loginResponse := httptest.NewRecorder()
	adminLoginPageHandler(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK || loginResponse.Body.Len() != len(adminLoginPage) {
		t.Fatalf("admin login page status=%d length=%d", loginResponse.Code, loginResponse.Body.Len())
	}
}

func TestAdminPageServesAfterAuthentication(t *testing.T) {
	resetAdminSessions(t)
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(adminCookieForTest(t))
	response := httptest.NewRecorder()
	adminHandler(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != len(adminPage) {
		t.Fatalf("admin page status=%d length=%d", response.Code, response.Body.Len())
	}
}

func TestAdminAPIsRejectUnauthenticatedRequests(t *testing.T) {
	resetAdminSessions(t)
	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
		body    string
	}{
		{name: "dashboard", handler: adminDashboardHandler, method: http.MethodGet, path: "/api/admin/dashboard"},
		{name: "doctors", handler: adminDoctorsHandler, method: http.MethodGet, path: "/api/admin/doctors"},
		{name: "audit", handler: adminAuditHandler, method: http.MethodGet, path: "/api/admin/audit"},
		{name: "records", handler: adminRecordsHandler, method: http.MethodGet, path: "/api/admin/records"},
		{name: "password", handler: adminPasswordHandler, method: http.MethodPost, path: "/api/admin/password", body: `{}`},
		{name: "platform config", handler: adminPlatformConfigHandler, method: http.MethodGet, path: "/api/admin/platform-config"},
		{name: "doctor reset password", handler: adminDoctorResetPasswordHandler, method: http.MethodPost, path: "/api/admin/doctors/reset-password", body: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			response := httptest.NewRecorder()
			test.handler(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
			}
		})
	}
}
