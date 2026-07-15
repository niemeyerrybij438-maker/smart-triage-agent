package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const adminSessionCookie = "aggo_admin_session"

type adminSession struct {
	Username    string
	DisplayName string
	ExpiresAt   time.Time
}

var adminSessionsMu sync.Mutex
var adminSessions = make(map[string]adminSession)

func newAdminSession(user StaffUser) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().Add(12 * time.Hour)
	session := adminSession{Username: user.Username, DisplayName: user.DisplayName, ExpiresAt: expiresAt}
	adminSessionsMu.Lock()
	adminSessions[token] = session
	adminSessionsMu.Unlock()
	savePersistentSession(token, persistentSessionAdmin, user.Username, user.DisplayName, expiresAt)
	return token, nil
}

func currentAdminSession(r *http.Request) (adminSession, bool) {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		return adminSession{}, false
	}
	adminSessionsMu.Lock()
	session, ok := adminSessions[cookie.Value]
	if ok && time.Now().After(session.ExpiresAt) {
		delete(adminSessions, cookie.Value)
		ok = false
	}
	adminSessionsMu.Unlock()
	if ok {
		return session, true
	}
	record, restored := loadPersistentSession(cookie.Value, persistentSessionAdmin)
	if !restored {
		return adminSession{}, false
	}
	session = adminSession{Username: record.Subject, DisplayName: record.DisplayName, ExpiresAt: record.ExpiresAt}
	adminSessionsMu.Lock()
	adminSessions[cookie.Value] = session
	adminSessionsMu.Unlock()
	return session, true
}

func revokeOtherAdminSessions(username, keepToken string) {
	adminSessionsMu.Lock()
	for token, session := range adminSessions {
		if session.Username == username && token != keepToken {
			delete(adminSessions, token)
		}
	}
	adminSessionsMu.Unlock()
	deleteOtherPersistentSessionsForSubject(persistentSessionAdmin, username, keepToken)
}

func requireAdminAPI(w http.ResponseWriter, r *http.Request) (adminSession, bool) {
	session, ok := currentAdminSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return adminSession{}, false
	}
	return session, true
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentAdminSession(r); !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	writeHTMLPage(w, adminPage)
}

func adminLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/login" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentAdminSession(r); ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	writeHTMLPage(w, adminLoginPage)
}

func adminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
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
	user, err := authenticateStaff(req.Username, req.Password, staffRoleAdmin)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, err := newAdminSession(user)
	if err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	writeAudit(user.DisplayName, "admin_login", user.Username, "")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": user.Username, "displayName": user.DisplayName})
}

func adminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		adminSessionsMu.Lock()
		delete(adminSessions, cookie.Value)
		adminSessionsMu.Unlock()
		deletePersistentSession(cookie.Value, persistentSessionAdmin)
	}
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func adminMeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": session.Username, "displayName": session.DisplayName})
}

func defaultPlatformConfig() PlatformConfig {
	return PlatformConfig{ID: 1, ServiceName: "\u667a\u533b\u5bfc\u8bca\u5e73\u53f0", DefaultLanguage: "zh-CN", SessionRetentionDays: 30, AlertLevel: "P1"}
}

func adminPlatformConfigHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.Method {
	case http.MethodGet:
		config := defaultPlatformConfig()
		err := globalDB.First(&config, 1).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			http.Error(w, "failed to load platform config", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(config)
	case http.MethodPost:
		var req PlatformConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.ServiceName = strings.TrimSpace(req.ServiceName)
		if req.ServiceName == "" || len([]rune(req.ServiceName)) > 128 {
			http.Error(w, "invalid service name", http.StatusBadRequest)
			return
		}
		if req.DefaultLanguage != "zh-CN" && req.DefaultLanguage != "en-US" {
			http.Error(w, "invalid default language", http.StatusBadRequest)
			return
		}
		if req.SessionRetentionDays < 1 || req.SessionRetentionDays > 3650 {
			http.Error(w, "session retention days must be between 1 and 3650", http.StatusBadRequest)
			return
		}
		if req.AlertLevel != "P1" && req.AlertLevel != "P2" && req.AlertLevel != "P3" {
			http.Error(w, "invalid alert level", http.StatusBadRequest)
			return
		}
		req.ID = 1
		var existing PlatformConfig
		err := globalDB.First(&existing, 1).Error
		if err == gorm.ErrRecordNotFound {
			if err := globalDB.Create(&req).Error; err != nil {
				http.Error(w, "failed to save platform config", http.StatusInternalServerError)
				return
			}
		} else if err != nil {
			http.Error(w, "failed to load platform config", http.StatusInternalServerError)
			return
		} else {
			existing.ServiceName = req.ServiceName
			existing.DefaultLanguage = req.DefaultLanguage
			existing.SessionRetentionDays = req.SessionRetentionDays
			existing.AlertLevel = req.AlertLevel
			if err := globalDB.Save(&existing).Error; err != nil {
				http.Error(w, "failed to save platform config", http.StatusInternalServerError)
				return
			}
			req = existing
		}
		writeAudit(session.DisplayName, "platform_config_update", "platform", fmt.Sprintf("language=%s retentionDays=%d alertLevel=%s", req.DefaultLanguage, req.SessionRetentionDays, req.AlertLevel))
		_ = json.NewEncoder(w).Encode(req)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := todayStart.AddDate(0, 0, 1)
	trendStart := todayStart.AddDate(0, 0, -6)
	_ = maybeGenerateFollowUpNotifications(now)

	type triageStats struct {
		Total     int64
		Today     int64
		Pending   int64
		Viewed    int64
		Processed int64
		P1        int64
		P2        int64
		P3        int64
	}
	var triage triageStats
	_ = globalDB.Model(&TriageRecord{}).Select(
		"COUNT(*) AS total, "+
			"SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END) AS today, "+
			"SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending, "+
			"SUM(CASE WHEN status = 'viewed' THEN 1 ELSE 0 END) AS viewed, "+
			"SUM(CASE WHEN status = 'processed' THEN 1 ELSE 0 END) AS processed, "+
			"SUM(CASE WHEN risk_level = 'P1' THEN 1 ELSE 0 END) AS p1, "+
			"SUM(CASE WHEN risk_level = 'P2' THEN 1 ELSE 0 END) AS p2, "+
			"SUM(CASE WHEN risk_level = 'P3' THEN 1 ELSE 0 END) AS p3",
		todayStart, tomorrow,
	).Scan(&triage).Error

	type followUpStats struct {
		Pending   int64
		Completed int64
		Overdue   int64
		Escalated int64
	}
	var followUps followUpStats
	_ = globalDB.Model(&FollowUpPlan{}).Select(
		"SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending, " +
			"SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed, " +
			"SUM(CASE WHEN status = 'overdue' THEN 1 ELSE 0 END) AS overdue, " +
			"SUM(CASE WHEN status = 'escalated' THEN 1 ELSE 0 END) AS escalated",
	).Scan(&followUps).Error

	type reminderStats struct {
		PatientUnread int64
		DoctorUnread  int64
		Today         int64
	}
	var reminders reminderStats
	_ = globalDB.Model(&FollowUpNotification{}).Select(
		"SUM(CASE WHEN recipient_type = ? AND is_read = ? THEN 1 ELSE 0 END) AS patient_unread, "+
			"SUM(CASE WHEN recipient_type = ? AND is_read = ? THEN 1 ELSE 0 END) AS doctor_unread, "+
			"SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END) AS today",
		followUpRecipientPatient, false, followUpRecipientDoctor, false, todayStart, tomorrow,
	).Scan(&reminders).Error

	var doctors int64
	_ = globalDB.Model(&StaffUser{}).Where("role = ? AND enabled = ?", staffRoleDoctor, true).Count(&doctors).Error

	type trendPoint struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}
	var trendRows []trendPoint
	_ = globalDB.Model(&TriageRecord{}).
		Select("DATE(created_at) AS date, COUNT(*) AS count").
		Where("created_at >= ? AND created_at < ?", trendStart, tomorrow).
		Group("DATE(created_at)").
		Order("date ASC").
		Scan(&trendRows).Error
	trendCounts := make(map[string]int64, len(trendRows))
	for _, row := range trendRows {
		trendCounts[row.Date] = row.Count
	}
	trend := make([]trendPoint, 0, 7)
	for offset := 6; offset >= 0; offset-- {
		date := todayStart.AddDate(0, 0, -offset).Format("2006-01-02")
		trend = append(trend, trendPoint{Date: date, Count: trendCounts[date]})
	}

	type departmentCount struct {
		Department string `json:"department"`
		Count      int64  `json:"count"`
	}
	var departments []departmentCount
	_ = globalDB.Model(&TriageRecord{}).Select("department, COUNT(*) AS count").Where("department <> ''").Group("department").Order("count DESC").Limit(6).Scan(&departments).Error

	var recentRisks []TriageRecord
	_ = globalDB.Where("risk_level IN ?", []string{"P1", "P2"}).Order("created_at DESC").Limit(6).Find(&recentRisks).Error
	for index := range recentRisks {
		recentRisks[index].Symptom = cleanSymptomDisplay(recentRisks[index].Symptom)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total": triage.Total, "today": triage.Today, "pending": triage.Pending, "viewed": triage.Viewed, "processed": triage.Processed,
		"p1": triage.P1, "p2": triage.P2, "p3": triage.P3, "doctors": doctors,
		"followUpPending": followUps.Pending, "followUpCompleted": followUps.Completed, "followUpOverdue": followUps.Overdue, "followUpEscalated": followUps.Escalated,
		"patientReminderUnread": reminders.PatientUnread, "doctorReminderUnread": reminders.DoctorUnread, "remindersToday": reminders.Today,
		"trend": trend, "departments": departments, "recentRisks": recentRisks,
	})
}

func adminDoctorsHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var users []StaffUser
		if err := globalDB.Where("role = ?", staffRoleDoctor).Order("created_at DESC").Find(&users).Error; err != nil {
			http.Error(w, "failed to query doctors", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(users)
	case http.MethodPost:
		var req struct {
			Username    string `json:"username"`
			Password    string `json:"password"`
			DisplayName string `json:"displayName"`
			Department  string `json:"department"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		req.DisplayName = strings.TrimSpace(req.DisplayName)
		if req.Username == "" || req.DisplayName == "" {
			http.Error(w, "username and displayName are required", http.StatusBadRequest)
			return
		}
		hash, err := hashPassword(req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		user := StaffUser{Username: req.Username, PasswordHash: hash, DisplayName: req.DisplayName, Role: staffRoleDoctor, Department: strings.TrimSpace(req.Department), Enabled: true}
		if err := globalDB.Create(&user).Error; err != nil {
			http.Error(w, "username already exists", http.StatusConflict)
			return
		}
		writeAudit(session.DisplayName, "doctor_create", user.Username, user.DisplayName)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(user)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminDoctorUpdateHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID          uint   `json:"id"`
		DisplayName string `json:"displayName"`
		Department  string `json:"department"`
		Password    string `json:"password"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	var user StaffUser
	if err := globalDB.Where("id = ? AND role = ?", req.ID, staffRoleDoctor).First(&user).Error; err != nil {
		http.Error(w, "doctor not found", http.StatusNotFound)
		return
	}
	updates := map[string]any{}
	if value := strings.TrimSpace(req.DisplayName); value != "" {
		updates["display_name"] = value
	}
	if req.Department != "" {
		updates["department"] = strings.TrimSpace(req.Department)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if strings.TrimSpace(req.Password) != "" {
		hash, err := hashPassword(req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updates["password_hash"] = hash
	}
	if len(updates) > 0 {
		if err := globalDB.Model(&user).Updates(updates).Error; err != nil {
			http.Error(w, "failed to update doctor", http.StatusInternalServerError)
			return
		}
		if _, passwordChanged := updates["password_hash"]; passwordChanged || (req.Enabled != nil && !*req.Enabled) {
			revokeDoctorSessions(user.Username)
		}
	}
	writeAudit(session.DisplayName, "doctor_update", user.Username, fmt.Sprintf("fields=%d", len(updates)))
	if err := globalDB.First(&user, user.ID).Error; err != nil && err != gorm.ErrRecordNotFound {
		http.Error(w, "failed to reload doctor", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(user)
}

func adminDoctorResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID       uint   `json:"id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var user StaffUser
	if err := globalDB.Where("id = ? AND role = ?", req.ID, staffRoleDoctor).First(&user).Error; err != nil {
		http.Error(w, "doctor not found", http.StatusNotFound)
		return
	}
	if err := globalDB.Model(&user).Update("password_hash", hash).Error; err != nil {
		http.Error(w, "failed to reset doctor password", http.StatusInternalServerError)
		return
	}
	revokeDoctorSessions(user.Username)
	writeAudit(session.DisplayName, "doctor_password_reset", user.Username, "sessions_revoked=true")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func adminRecordsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := globalDB.Model(&TriageRecord{}).Select("id, session_id, symptom, risk_level, risk_text, department, status, handled_by, created_at").Order("created_at DESC")
	if risk := strings.TrimSpace(r.URL.Query().Get("risk")); risk != "" {
		query = query.Where("risk_level = ?", risk)
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("symptom LIKE ? OR department LIKE ? OR session_id LIKE ?", like, like, like)
	}
	var records []TriageRecord
	if err := query.Limit(200).Find(&records).Error; err != nil {
		http.Error(w, "failed to query records", http.StatusInternalServerError)
		return
	}
	for index := range records {
		records[index].Symptom = cleanSymptomDisplay(records[index].Symptom)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(records)
}

func adminPasswordHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	var user StaffUser
	if err := globalDB.Where("username = ? AND role = ?", session.Username, staffRoleAdmin).First(&user).Error; err != nil || !verifyPassword(user.PasswordHash, req.CurrentPassword) {
		http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := globalDB.Model(&user).Update("password_hash", hash).Error; err != nil {
		http.Error(w, "failed to update password", http.StatusInternalServerError)
		return
	}
	keepToken := ""
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		keepToken = cookie.Value
	}
	revokeOtherAdminSessions(user.Username, keepToken)
	writeAudit(session.DisplayName, "admin_password_update", user.Username, "")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func adminAuditHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	var logs []AuditLog
	if err := globalDB.Order("created_at DESC").Limit(100).Find(&logs).Error; err != nil {
		http.Error(w, "failed to query audit logs", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}
