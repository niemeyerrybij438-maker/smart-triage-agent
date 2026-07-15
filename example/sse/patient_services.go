package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CoolBanHub/aggo/utils"
	"github.com/phpdave11/gofpdf"
	"gorm.io/gorm"
)

const patientSessionCookie = "aggo_patient_session"

type PatientProfile struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Phone            string    `gorm:"size:32;uniqueIndex" json:"phone"`
	DisplayName      string    `gorm:"size:128" json:"displayName"`
	Gender           string    `gorm:"size:16" json:"gender"`
	Age              int       `json:"age"`
	HeightCM         float64   `json:"heightCm"`
	WeightKG         float64   `json:"weightKg"`
	BloodType        string    `gorm:"size:16" json:"bloodType"`
	ChronicDiseases  string    `gorm:"size:500" json:"chronicDiseases"`
	DrugAllergies    string    `gorm:"size:500" json:"drugAllergies"`
	MedicalHistory   string    `gorm:"size:1000" json:"medicalHistory"`
	EmergencyName    string    `gorm:"size:128" json:"emergencyName"`
	EmergencyPhone   string    `gorm:"size:32" json:"emergencyPhone"`
	BloodPressure    string    `gorm:"size:32" json:"bloodPressure"`
	RestingHeartRate int       `json:"restingHeartRate"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Appointment struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	BookingNo     string    `gorm:"size:32;uniqueIndex" json:"bookingNo"`
	SessionID     string    `gorm:"size:64;index" json:"sessionId"`
	PatientPhone  string    `gorm:"size:32;index" json:"patientPhone"`
	PatientName   string    `gorm:"size:128" json:"patientName"`
	Hospital      string    `gorm:"size:255;index" json:"hospital"`
	Department    string    `gorm:"size:128;index" json:"department"`
	AppointmentAt time.Time `gorm:"index" json:"appointmentAt"`
	Status        string    `gorm:"size:24;index" json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
}

type patientSession struct {
	Phone       string
	DisplayName string
	ExpiresAt   time.Time
}

type smsCode struct {
	Code      string
	ExpiresAt time.Time
	SentAt    time.Time
	Attempts  int
}

var patientSessionsMu sync.Mutex
var patientSessions = map[string]patientSession{}
var smsCodesMu sync.Mutex
var smsCodes = map[string]smsCode{}

type HospitalLocation struct {
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	DistanceKM  float64  `json:"distanceKm"`
	Departments []string `json:"departments"`
}

var defaultHospitals = []HospitalLocation{
	{Name: "\u5bbf\u8fc1\u5e02\u4e2d\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u5bbf\u8c6b\u533a\u6d2a\u6cfd\u6e56\u4e1c\u8def 9 \u53f7", Latitude: 33.953762, Longitude: 118.330213, Departments: []string{"\u6025\u8bca\u79d1", "\u547c\u5438\u5185\u79d1", "\u6d88\u5316\u5185\u79d1", "\u5fc3\u8840\u7ba1\u5185\u79d1", "\u5168\u79d1\u533b\u5b66\u79d1"}},
	{Name: "\u5bbf\u8fc1\u949f\u543e\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u7ecf\u6d4e\u6280\u672f\u5f00\u53d1\u533a\u53d1\u5c55\u5927\u9053 3366 \u53f7", Latitude: 33.961500, Longitude: 118.286500, Departments: []string{"\u6025\u8bca\u79d1", "\u547c\u5438\u5185\u79d1", "\u795e\u7ecf\u5185\u79d1", "\u9aa8\u79d1", "\u5168\u79d1\u533b\u5b66\u79d1"}},
	{Name: "\u6c5f\u82cf\u7701\u4eba\u6c11\u533b\u9662\u5bbf\u8fc1\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u5bbf\u57ce\u533a\u5bbf\u652f\u8def 120 \u53f7", Latitude: 33.967100, Longitude: 118.267800, Departments: []string{"\u6025\u8bca\u79d1", "\u547c\u5438\u5185\u79d1", "\u6d88\u5316\u5185\u79d1", "\u5fc3\u8840\u7ba1\u5185\u79d1", "\u5168\u79d1\u533b\u5b66\u79d1"}},
}

func patientServicesModels() []any { return []any{&PatientProfile{}, &Appointment{}} }

func randomDigits(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, value := range raw {
		builder.WriteByte('0' + value%10)
	}
	return builder.String(), nil
}

func normalizePhone(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, " ", ""))
}
func validPhone(value string) bool {
	if len(value) != 11 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func patientSMSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Phone string `json:"phone"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	phone := normalizePhone(req.Phone)
	if !validPhone(phone) {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	smsCodesMu.Lock()
	existing := smsCodes[phone]
	if time.Since(existing.SentAt) < time.Minute {
		smsCodesMu.Unlock()
		http.Error(w, "please wait before requesting another code", http.StatusTooManyRequests)
		return
	}
	code, err := randomDigits(6)
	if err != nil {
		smsCodesMu.Unlock()
		http.Error(w, "code generation failed", http.StatusInternalServerError)
		return
	}
	smsCodes[phone] = smsCode{Code: code, ExpiresAt: time.Now().Add(5 * time.Minute), SentAt: time.Now()}
	smsCodesMu.Unlock()
	isProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	if isProduction {
		if err := deliverSMS(phone, code); err != nil {
			http.Error(w, "sms delivery failed", http.StatusBadGateway)
			return
		}
	}
	result := map[string]any{"ok": true, "expiresIn": 300, "deliveryMode": "local"}
	if isProduction {
		result["deliveryMode"] = "sms"
	} else {
		result["debugCode"] = code
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func deliverSMS(phone, code string) error {
	account, password := strings.TrimSpace(os.Getenv("IHUYI_ACCOUNT")), strings.TrimSpace(os.Getenv("IHUYI_PASSWORD"))
	if account != "" && password != "" {
		return deliverIhuyiSMS(phone, code, account, password)
	}
	endpoint := strings.TrimSpace(os.Getenv("SMS_PROVIDER_URL"))
	if endpoint == "" {
		if strings.ToLower(os.Getenv("APP_ENV")) == "production" {
			return fmt.Errorf("SMS provider is not configured")
		}
		return nil
	}
	body, _ := json.Marshal(map[string]string{"phone": phone, "code": code, "template": "patient_login"})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("SMS_PROVIDER_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider status %d", resp.StatusCode)
	}
	return nil
}

func deliverIhuyiSMS(phone, code, account, password string) error {
	form := url.Values{}
	form.Set("account", account)
	form.Set("password", password)
	form.Set("mobile", phone)
	form.Set("templateid", envOrDefault("IHUYI_TEMPLATE_ID", "1"))
	form.Set("content", code)
	form.Set("format", "json")
	req, err := http.NewRequest(http.MethodPost, "https://106.ihuyi.com/webservice/sms.php?method=Submit", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("invalid ihuyi response: %w", err)
	}
	if result.Code != 2 {
		return fmt.Errorf("ihuyi sms failed: code=%d msg=%s", result.Code, result.Msg)
	}
	return nil
}

func patientLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	phone := normalizePhone(req.Phone)
	smsCodesMu.Lock()
	entry, ok := smsCodes[phone]
	if ok && entry.Attempts >= 5 {
		delete(smsCodes, phone)
		smsCodesMu.Unlock()
		http.Error(w, "verification attempts exceeded", http.StatusTooManyRequests)
		return
	}
	if !ok || time.Now().After(entry.ExpiresAt) || entry.Code != strings.TrimSpace(req.Code) {
		if ok {
			entry.Attempts++
			smsCodes[phone] = entry
		}
		smsCodesMu.Unlock()
		http.Error(w, "invalid or expired code", http.StatusUnauthorized)
		return
	}
	delete(smsCodes, phone)
	smsCodesMu.Unlock()
	var profile PatientProfile
	if err := globalDB.Where("phone = ?", phone).First(&profile).Error; err != nil {
		profile = PatientProfile{Phone: phone, DisplayName: "\u9648\u6653"}
		if err := globalDB.Create(&profile).Error; err != nil {
			http.Error(w, "profile creation failed", http.StatusInternalServerError)
			return
		}
	}
	if profile.DisplayName != "\u9648\u6653" {
		profile.DisplayName = "\u9648\u6653"
		if err := globalDB.Model(&profile).Update("display_name", profile.DisplayName).Error; err != nil {
			http.Error(w, "profile update failed", http.StatusInternalServerError)
			return
		}
	}
	token := utils.GetULID()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	session := patientSession{Phone: phone, DisplayName: profile.DisplayName, ExpiresAt: expiresAt}
	patientSessionsMu.Lock()
	patientSessions[token] = session
	patientSessionsMu.Unlock()
	savePersistentSession(token, persistentSessionPatient, phone, profile.DisplayName, expiresAt)
	http.SetCookie(w, &http.Cookie{Name: patientSessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func currentPatient(r *http.Request) (patientSession, bool) {
	cookie, err := r.Cookie(patientSessionCookie)
	if err != nil || cookie.Value == "" {
		return patientSession{}, false
	}
	patientSessionsMu.Lock()
	session, ok := patientSessions[cookie.Value]
	if ok && time.Now().After(session.ExpiresAt) {
		delete(patientSessions, cookie.Value)
		ok = false
	}
	patientSessionsMu.Unlock()
	if ok {
		return session, true
	}
	record, restored := loadPersistentSession(cookie.Value, persistentSessionPatient)
	if !restored {
		return patientSession{}, false
	}
	session = patientSession{Phone: record.Subject, DisplayName: record.DisplayName, ExpiresAt: record.ExpiresAt}
	patientSessionsMu.Lock()
	patientSessions[cookie.Value] = session
	patientSessionsMu.Unlock()
	return session, true
}

func patientLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/patient/login" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentPatient(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeHTMLPage(w, patientLoginPage)
}

func patientLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(patientSessionCookie); err == nil {
		patientSessionsMu.Lock()
		delete(patientSessions, cookie.Value)
		patientSessionsMu.Unlock()
		deletePersistentSession(cookie.Value, persistentSessionPatient)
	}
	http.SetCookie(w, &http.Cookie{Name: patientSessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func mapConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"provider": "baidu", "ak": strings.TrimSpace(os.Getenv("BAIDU_MAP_AK"))})
}

func patientMeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"phone": session.Phone, "displayName": session.DisplayName})
}

type triagePatientProfileSnapshot struct {
	DisplayName      string  `json:"displayName"`
	Gender           string  `json:"gender"`
	Age              int     `json:"age"`
	HeightCM         float64 `json:"heightCm"`
	WeightKG         float64 `json:"weightKg"`
	BloodType        string  `json:"bloodType"`
	ChronicDiseases  string  `json:"chronicDiseases"`
	DrugAllergies    string  `json:"drugAllergies"`
	MedicalHistory   string  `json:"medicalHistory"`
	EmergencyName    string  `json:"emergencyName"`
	EmergencyPhone   string  `json:"emergencyPhone"`
	BloodPressure    string  `json:"bloodPressure"`
	RestingHeartRate int     `json:"restingHeartRate"`
}

func loadPatientProfileByPhone(phone string) (PatientProfile, error) {
	var profile PatientProfile
	err := globalDB.Where("phone = ?", strings.TrimSpace(phone)).First(&profile).Error
	return profile, err
}

func patientProfileSnapshotJSON(phone string) string {
	if globalDB == nil || strings.TrimSpace(phone) == "" {
		return ""
	}
	profile, err := loadPatientProfileByPhone(phone)
	if err != nil {
		return ""
	}
	snapshot := triagePatientProfileSnapshot{DisplayName: profile.DisplayName, Gender: profile.Gender, Age: profile.Age, HeightCM: profile.HeightCM, WeightKG: profile.WeightKG, BloodType: profile.BloodType, ChronicDiseases: profile.ChronicDiseases, DrugAllergies: profile.DrugAllergies, MedicalHistory: profile.MedicalHistory, EmergencyName: profile.EmergencyName, EmergencyPhone: profile.EmergencyPhone, BloodPressure: profile.BloodPressure, RestingHeartRate: profile.RestingHeartRate}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

func mergePatientProfileSnapshot(snapshotJSON, phone string) string {
	return mergePatientProfileSnapshotJSON(snapshotJSON, patientProfileSnapshotJSON(phone))
}

func mergePatientProfileSnapshotJSON(snapshotJSON, currentJSON string) string {
	if strings.TrimSpace(currentJSON) == "" {
		return snapshotJSON
	}
	var current triagePatientProfileSnapshot
	if json.Unmarshal([]byte(currentJSON), &current) != nil {
		return snapshotJSON
	}
	var snapshot triagePatientProfileSnapshot
	if strings.TrimSpace(snapshotJSON) != "" && json.Unmarshal([]byte(snapshotJSON), &snapshot) != nil {
		return snapshotJSON
	}
	if snapshot.DisplayName == "" {
		snapshot.DisplayName = current.DisplayName
	}
	if snapshot.Gender == "" {
		snapshot.Gender = current.Gender
	}
	if snapshot.Age == 0 {
		snapshot.Age = current.Age
	}
	if snapshot.HeightCM == 0 {
		snapshot.HeightCM = current.HeightCM
	}
	if snapshot.WeightKG == 0 {
		snapshot.WeightKG = current.WeightKG
	}
	if snapshot.BloodType == "" {
		snapshot.BloodType = current.BloodType
	}
	if snapshot.ChronicDiseases == "" {
		snapshot.ChronicDiseases = current.ChronicDiseases
	}
	if snapshot.DrugAllergies == "" {
		snapshot.DrugAllergies = current.DrugAllergies
	}
	if snapshot.MedicalHistory == "" {
		snapshot.MedicalHistory = current.MedicalHistory
	}
	if snapshot.EmergencyName == "" {
		snapshot.EmergencyName = current.EmergencyName
	}
	if snapshot.EmergencyPhone == "" {
		snapshot.EmergencyPhone = current.EmergencyPhone
	}
	if snapshot.BloodPressure == "" {
		snapshot.BloodPressure = current.BloodPressure
	}
	if snapshot.RestingHeartRate == 0 {
		snapshot.RestingHeartRate = current.RestingHeartRate
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return snapshotJSON
	}
	return string(data)
}

func patientProfileAIContext(phone string) string {
	if globalDB == nil || strings.TrimSpace(phone) == "" {
		return ""
	}
	profile, err := loadPatientProfileByPhone(phone)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, 8)
	if profile.Gender != "" {
		parts = append(parts, "\u6027\u522b\uff1a"+profile.Gender)
	}
	if profile.Age > 0 {
		parts = append(parts, fmt.Sprintf("\u5e74\u9f84\uff1a%d\u5c81", profile.Age))
	}
	if profile.ChronicDiseases != "" {
		parts = append(parts, "\u6162\u75c5\uff1a"+profile.ChronicDiseases)
	}
	if profile.DrugAllergies != "" {
		parts = append(parts, "\u836f\u7269\u8fc7\u654f\uff1a"+profile.DrugAllergies)
	}
	if profile.MedicalHistory != "" {
		parts = append(parts, "\u65e2\u5f80\u53f2\uff1a"+profile.MedicalHistory)
	}
	if profile.BloodPressure != "" {
		parts = append(parts, "\u8840\u538b\u8bb0\u5f55\uff1a"+profile.BloodPressure)
	}
	if profile.RestingHeartRate > 0 {
		parts = append(parts, fmt.Sprintf("\u9759\u606f\u5fc3\u7387\uff1a%d\u6b21/\u5206\u949f", profile.RestingHeartRate))
	}
	if len(parts) == 0 {
		return ""
	}
	return "<patient_health_profile>\n" + strings.Join(parts, "\n") + "\n</patient_health_profile>\n\u4ee5\u4e0a\u4e3a\u60a3\u8005\u5df2\u7ef4\u62a4\u7684\u5065\u5eb7\u6863\u6848\uff0c\u8bf7\u7ed3\u5408\u5f53\u524d\u4e3b\u8bc9\u8fdb\u884c\u98ce\u9669\u8bc4\u4f30\uff1b\u82e5\u6863\u6848\u4e0e\u5f53\u524d\u63cf\u8ff0\u51b2\u7a81\uff0c\u5e94\u5411\u60a3\u8005\u6838\u5b9e\u3002"
}

func patientProfileHandler(w http.ResponseWriter, r *http.Request) {
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	session, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var profile PatientProfile
	if err := globalDB.Where("phone = ?", session.Phone).First(&profile).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			http.Error(w, "profile query failed", http.StatusInternalServerError)
			return
		}
		profile = PatientProfile{Phone: session.Phone, DisplayName: "\u9648\u6653"}
		if err := globalDB.Create(&profile).Error; err != nil {
			http.Error(w, "profile creation failed", http.StatusInternalServerError)
			return
		}
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(profile)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input PatientProfile
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	input.Gender = strings.TrimSpace(input.Gender)
	input.BloodType = strings.TrimSpace(input.BloodType)
	input.ChronicDiseases = strings.TrimSpace(input.ChronicDiseases)
	input.DrugAllergies = strings.TrimSpace(input.DrugAllergies)
	input.MedicalHistory = strings.TrimSpace(input.MedicalHistory)
	input.EmergencyName = strings.TrimSpace(input.EmergencyName)
	input.EmergencyPhone = normalizePhone(input.EmergencyPhone)
	input.BloodPressure = strings.TrimSpace(input.BloodPressure)
	if input.Age < 0 || input.Age > 120 || input.HeightCM < 0 || input.HeightCM > 250 || input.WeightKG < 0 || input.WeightKG > 300 || input.RestingHeartRate < 0 || input.RestingHeartRate > 240 {
		http.Error(w, "profile values out of range", http.StatusBadRequest)
		return
	}
	if (input.HeightCM > 0 && input.HeightCM < 50) || (input.WeightKG > 0 && input.WeightKG < 2) || (input.RestingHeartRate > 0 && input.RestingHeartRate < 20) {
		http.Error(w, "profile values out of range", http.StatusBadRequest)
		return
	}
	if input.EmergencyPhone != "" && !validPhone(input.EmergencyPhone) {
		http.Error(w, "invalid emergency phone", http.StatusBadRequest)
		return
	}
	if len([]rune(input.ChronicDiseases)) > 500 || len([]rune(input.DrugAllergies)) > 500 || len([]rune(input.MedicalHistory)) > 1000 || len([]rune(input.EmergencyName)) > 128 || len([]rune(input.BloodPressure)) > 32 {
		http.Error(w, "profile text too long", http.StatusBadRequest)
		return
	}
	updates := map[string]any{
		"display_name":       "\u9648\u6653",
		"gender":             input.Gender,
		"age":                input.Age,
		"height_cm":          input.HeightCM,
		"weight_kg":          input.WeightKG,
		"blood_type":         input.BloodType,
		"chronic_diseases":   input.ChronicDiseases,
		"drug_allergies":     input.DrugAllergies,
		"medical_history":    input.MedicalHistory,
		"emergency_name":     input.EmergencyName,
		"emergency_phone":    input.EmergencyPhone,
		"blood_pressure":     input.BloodPressure,
		"resting_heart_rate": input.RestingHeartRate,
	}
	if err := globalDB.Model(&profile).Updates(updates).Error; err != nil {
		http.Error(w, "profile update failed", http.StatusInternalServerError)
		return
	}
	profile = PatientProfile{}
	if err := globalDB.Where("phone = ?", session.Phone).First(&profile).Error; err != nil {
		http.Error(w, "profile query failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func appointmentAccess(r *http.Request) (patientSession, string, string, bool) {
	if doctor, ok := currentDoctor(r); ok {
		return patientSession{}, "doctor", doctor, true
	}
	if admin, ok := currentAdminSession(r); ok {
		return patientSession{}, "admin", admin.Username, true
	}
	if patient, ok := currentPatient(r); ok {
		return patient, "patient", "", true
	}
	return patientSession{}, "", "", false
}

func appointmentStatusHandler(w http.ResponseWriter, r *http.Request) {
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	patient, role, actor, ok := appointmentAccess(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		BookingNo string `json:"bookingNo"`
		Status    string `json:"status"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.BookingNo) == "" {
		http.Error(w, "invalid appointment request", http.StatusBadRequest)
		return
	}
	var row Appointment
	if err := globalDB.Where("booking_no = ?", strings.TrimSpace(req.BookingNo)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "appointment not found", http.StatusNotFound)
		} else {
			http.Error(w, "query failed", http.StatusInternalServerError)
		}
		return
	}
	status := strings.TrimSpace(req.Status)
	if role == "patient" {
		if row.PatientPhone != patient.Phone {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if status != "canceled" || row.Status != "booked" {
			http.Error(w, "appointment cannot be canceled", http.StatusConflict)
			return
		}
		actor = patient.DisplayName
	} else {
		allowed := (row.Status == "booked" && (status == "confirmed" || status == "canceled")) || (row.Status == "confirmed" && (status == "completed" || status == "canceled"))
		if !allowed {
			http.Error(w, "invalid appointment status transition", http.StatusConflict)
			return
		}
	}
	if err := globalDB.Model(&row).Update("status", status).Error; err != nil {
		http.Error(w, "status update failed", http.StatusInternalServerError)
		return
	}
	row.Status = status
	writeAudit(actor, "appointment_"+status, row.BookingNo, row.Department)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(row)
}

func reconcileAppointmentHospitals() {
	if globalDB == nil {
		return
	}
	updates := map[string]struct {
		Name    string
		Address string
	}{
		"\u5e02\u7b2c\u4e00\u4eba\u6c11\u533b\u9662":       {Name: "\u5bbf\u8fc1\u949f\u543e\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u7ecf\u6d4e\u6280\u672f\u5f00\u53d1\u533a\u53d1\u5c55\u5927\u9053 3366 \u53f7"},
		"\u5e02\u4e2d\u5fc3\u533b\u9662":                   {Name: "\u6c5f\u82cf\u7701\u4eba\u6c11\u533b\u9662\u5bbf\u8fc1\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u5bbf\u57ce\u533a\u5bbf\u652f\u8def 120 \u53f7"},
		"\u5e02\u4e2d\u897f\u533b\u7ed3\u5408\u533b\u9662": {Name: "\u5bbf\u8fc1\u5e02\u4e2d\u533b\u9662", Address: "\u5bbf\u8fc1\u5e02\u5bbf\u8c6b\u533a\u6d2a\u6cfd\u6e56\u4e1c\u8def 9 \u53f7"},
	}
	for oldName, target := range updates {
		_ = globalDB.Model(&Appointment{}).Where("hospital = ?", oldName).Updates(map[string]any{"hospital": target.Name}).Error
	}
	_ = globalDB.Model(&PatientProfile{}).Where("display_name <> ? OR display_name = ''", "\u9648\u6653").Update("display_name", "\u9648\u6653").Error
	_ = globalDB.Model(&Appointment{}).Where("patient_name <> ? OR patient_name = ''", "\u9648\u6653").Update("patient_name", "\u9648\u6653").Error
}

func appointmentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		patient, role, _, ok := appointmentAccess(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		query := globalDB.Order("appointment_at DESC")
		if role == "patient" {
			query = query.Where("patient_phone = ?", patient.Phone)
		}
		if sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId")); sessionID != "" {
			query = query.Where("session_id = ?", sessionID)
		}
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			query = query.Where("status = ?", status)
		}
		var rows []Appointment
		if query.Limit(100).Find(&rows).Error != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	case http.MethodPost:
		patient, ok := currentPatient(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			SessionID     string `json:"sessionId"`
			PatientName   string `json:"patientName"`
			Phone         string `json:"phone"`
			Hospital      string `json:"hospital"`
			Department    string `json:"department"`
			AppointmentAt string `json:"appointmentAt"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Hospital) == "" || strings.TrimSpace(req.Department) == "" {
			http.Error(w, "missing booking fields", http.StatusBadRequest)
			return
		}
		at, err := time.Parse(time.RFC3339, req.AppointmentAt)
		if err != nil || at.Before(time.Now().Add(-time.Minute)) {
			http.Error(w, "invalid appointment time", http.StatusBadRequest)
			return
		}
		bookingNo := "AP" + time.Now().Format("060102") + strings.ToUpper(utils.GetULID()[18:])
		row := Appointment{BookingNo: bookingNo, SessionID: strings.TrimSpace(req.SessionID), PatientPhone: patient.Phone, PatientName: strings.TrimSpace(patient.DisplayName), Hospital: strings.TrimSpace(req.Hospital), Department: strings.TrimSpace(req.Department), AppointmentAt: at, Status: "booked"}
		if globalDB.Create(&row).Error != nil {
			http.Error(w, "booking failed", http.StatusInternalServerError)
			return
		}
		writeAudit(row.PatientName, "appointment_create", row.BookingNo, row.Department)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(row)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func hospitalLocationsHandler(w http.ResponseWriter, r *http.Request) {
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, err2 := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "valid lat and lng are required", http.StatusBadRequest)
		return
	}
	department := strings.TrimSpace(r.URL.Query().Get("department"))
	rows := make([]HospitalLocation, 0, len(defaultHospitals))
	for _, hospital := range defaultHospitals {
		if department != "" && !containsString(hospital.Departments, department) {
			continue
		}
		hospital.DistanceKM = math.Round(haversine(lat, lng, hospital.Latitude, hospital.Longitude)*10) / 10
		rows = append(rows, hospital)
	}
	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].DistanceKM < rows[i].DistanceKM {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func containsString(rows []string, target string) bool {
	for _, value := range rows {
		if value == target {
			return true
		}
	}
	return false
}
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earth = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earth * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func triageReportPDFHandler(w http.ResponseWriter, r *http.Request) {
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	id, _ := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
	var record TriageRecord
	query := globalDB
	if id > 0 {
		query = query.Where("id = ?", id)
	} else if sessionID != "" {
		query = query.Where("session_id = ?", sessionID).Order("created_at DESC")
	} else {
		http.Error(w, "id or sessionId is required", http.StatusBadRequest)
		return
	}
	if query.First(&record).Error != nil {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}
	if _, ok := currentDoctor(r); !ok {
		if _, ok := currentAdminSession(r); !ok {
			patient, patientOK := currentPatient(r)
			if !patientOK || strings.TrimSpace(record.PatientPhone) == "" || record.PatientPhone != patient.Phone {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	font := findChineseFont()
	if font != "" {
		pdf.AddUTF8Font("cn", "", font)
		pdf.AddUTF8Font("cn", "B", font)
		pdf.SetFont("cn", "B", 18)
	} else {
		pdf.SetFont("Arial", "B", 18)
	}
	pdf.AddPage()
	pdf.CellFormat(0, 12, "\u667a\u80fd\u5bfc\u8bca\u98ce\u9669\u8bc4\u4f30\u62a5\u544a", "", 1, "C", false, 0, "")
	if font != "" {
		pdf.SetFont("cn", "", 11)
	} else {
		pdf.SetFont("Arial", "", 11)
	}
	lines := [][2]string{{"\u62a5\u544a\u7f16\u53f7", record.SessionID}, {"\u751f\u6210\u65f6\u95f4", record.CreatedAt.Format("2006-01-02 15:04")}, {"\u4e3b\u8981\u75c7\u72b6", cleanSymptomDisplay(record.Symptom)}, {"\u98ce\u9669\u7b49\u7ea7", record.RiskLevel + " " + record.RiskText}, {"\u63a8\u8350\u79d1\u5ba4", record.Department}, {"\u5907\u9009\u79d1\u5ba4", record.Alternatives}, {"\u5224\u65ad\u4f9d\u636e", record.Reason}, {"AI \u53c2\u8003\u4f9d\u636e", ragEvidenceSourceSummary(record.RAGEvidence)}, {"\u5c31\u8bca\u51c6\u5907", record.Preparation}, {"\u98ce\u9669\u63d0\u9192", record.Warning}, {"\u533b\u751f\u610f\u89c1", record.DoctorNote}}
	for _, line := range lines {
		pdf.SetFontStyle("B")
		pdf.CellFormat(32, 8, line[0]+"\uff1a", "", 0, "L", false, 0, "")
		pdf.SetFontStyle("")
		pdf.MultiCell(0, 8, valueOrDash(line[1]), "", "L", false)
	}
	pdf.Ln(5)
	pdf.SetTextColor(110, 110, 110)
	pdf.MultiCell(0, 7, "\u672c\u62a5\u544a\u7531\u667a\u80fd\u5bfc\u8bca\u7cfb\u7edf\u751f\u6210\uff0c\u4ec5\u4f9b\u5c31\u533b\u53c2\u8003\uff0c\u4e0d\u80fd\u66ff\u4ee3\u533b\u751f\u9762\u8bca\u548c\u6b63\u5f0f\u533b\u5b66\u8bca\u65ad\u3002", "", "L", false)
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		http.Error(w, "pdf generation failed", http.StatusInternalServerError)
		return
	}
	filename := "triage-report-" + record.SessionID + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(out.Len()))
	_, _ = w.Write(out.Bytes())
}

func ragEvidenceSourceSummary(value string) string {
	var rows []medicalKnowledgeHit
	if json.Unmarshal([]byte(strings.TrimSpace(value)), &rows) != nil || len(rows) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = strings.TrimSpace(row.Code)
		}
		source := strings.TrimSpace(row.SourceName)
		if source == "" {
			source = "公开医疗知识"
		}
		parts = append(parts, fmt.Sprintf("%s v%d（%s）", title, max(row.Version, 1), source))
	}
	return strings.Join(parts, "；")
}
func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}
func findChineseFont() string {
	for _, candidate := range []string{os.Getenv("PDF_FONT_PATH"), `C:\Windows\Fonts\Deng.ttf`, `C:\Windows\Fonts\simhei.ttf`, `/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc`, `/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc`} {
		if candidate != "" {
			if info, err := os.Stat(filepath.Clean(candidate)); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}
