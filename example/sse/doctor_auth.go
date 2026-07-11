package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const doctorSessionCookie = "aggo_doctor_session"

type doctorSession struct {
	Username    string
	DisplayName string
	ExpiresAt   time.Time
}

var doctorSessionsMu sync.Mutex
var doctorSessions = make(map[string]doctorSession)

func doctorDisplayName() string {
	displayName := strings.TrimSpace(os.Getenv("DOCTOR_DISPLAY_NAME"))
	if displayName == "" {
		displayName = "\u674e\u533b\u751f"
	}
	return displayName
}

func doctorCredentials() (string, string) {
	username := strings.TrimSpace(os.Getenv("DOCTOR_USERNAME"))
	password := os.Getenv("DOCTOR_PASSWORD")
	if username == "" {
		username = "doctor"
	}
	if password == "" {
		password = "doctor123"
	}
	return username, password
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func newDoctorSession(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	doctorSessionsMu.Lock()
	doctorSessions[token] = doctorSession{Username: username, DisplayName: doctorDisplayName(), ExpiresAt: time.Now().Add(12 * time.Hour)}
	doctorSessionsMu.Unlock()
	return token, nil
}

func currentDoctorSession(r *http.Request) (doctorSession, bool) {
	cookie, err := r.Cookie(doctorSessionCookie)
	if err != nil || cookie.Value == "" {
		return doctorSession{}, false
	}
	doctorSessionsMu.Lock()
	defer doctorSessionsMu.Unlock()
	session, ok := doctorSessions[cookie.Value]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(doctorSessions, cookie.Value)
		return doctorSession{}, false
	}
	return session, true
}

func currentDoctor(r *http.Request) (string, bool) {
	session, ok := currentDoctorSession(r)
	if !ok {
		return "", false
	}
	return session.Username, true
}

func requireDoctorAPI(w http.ResponseWriter, r *http.Request) (doctorSession, bool) {
	session, ok := currentDoctorSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return doctorSession{}, false
	}
	return session, true
}

func doctorLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/doctor/login" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentDoctor(r); ok {
		http.Redirect(w, r, "/doctor", http.StatusSeeOther)
		return
	}
	writeHTMLPage(w, doctorLoginPage)
}

func doctorLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	expectedUsername, expectedPassword := doctorCredentials()
	if !secureEqual(strings.TrimSpace(req.Username), expectedUsername) || !secureEqual(req.Password, expectedPassword) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, err := newDoctorSession(expectedUsername)
	if err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: doctorSessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": expectedUsername})
}

func doctorLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(doctorSessionCookie); err == nil {
		doctorSessionsMu.Lock()
		delete(doctorSessions, cookie.Value)
		doctorSessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: doctorSessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func doctorMeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireDoctorAPI(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": session.Username, "displayName": session.DisplayName})
}

func doctorHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/doctor" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentDoctor(r); !ok {
		http.Redirect(w, r, "/doctor/login", http.StatusSeeOther)
		return
	}
	writeHTMLPage(w, doctorPage)
}
