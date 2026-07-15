package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPatientChatsRequiresLogin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/patient/chats", nil)
	response := httptest.NewRecorder()
	patientChatsHandler(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestPatientChatPayloadShape(t *testing.T) {
	body := `{"key":"chat-1","title":"test","messages":[{"user":true,"text":"hello"}],"progress":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/patient/chats", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: "invalid", Expires: time.Now().Add(time.Hour)})
	response := httptest.NewRecorder()
	patientChatsHandler(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}
