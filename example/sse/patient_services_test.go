package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func resetPatientServices(t *testing.T) {
	t.Helper()
	smsCodesMu.Lock()
	oldCodes := smsCodes
	smsCodes = map[string]smsCode{}
	smsCodesMu.Unlock()
	patientSessionsMu.Lock()
	oldSessions := patientSessions
	patientSessions = map[string]patientSession{}
	patientSessionsMu.Unlock()
	t.Cleanup(func() {
		smsCodesMu.Lock()
		smsCodes = oldCodes
		smsCodesMu.Unlock()
		patientSessionsMu.Lock()
		patientSessions = oldSessions
		patientSessionsMu.Unlock()
	})
}

func TestPatientSMSDevelopmentCodeLifecycle(t *testing.T) {
	resetPatientServices(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SMS_PROVIDER_URL", "")
	t.Setenv("IHUYI_ACCOUNT", "configured-but-disabled-in-development")
	t.Setenv("IHUYI_PASSWORD", "configured-but-disabled-in-development")
	req := httptest.NewRequest(http.MethodPost, "/api/patient/sms", bytes.NewBufferString(`{"phone":"13800138000"}`))
	res := httptest.NewRecorder()
	patientSMSHandler(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	code, _ := body["debugCode"].(string)
	if len(code) != 6 {
		t.Fatalf("debug code=%q", code)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/api/patient/sms", bytes.NewBufferString(`{"phone":"13800138000"}`))
	res2 := httptest.NewRecorder()
	patientSMSHandler(res2, req2)
	if res2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status=%d", res2.Code)
	}
}

func TestNearbyHospitalsSortedByDistance(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/hospitals/nearby?lat=39.9042&lng=116.4074&department=%E6%80%A5%E8%AF%8A%E7%A7%91", nil)
	res := httptest.NewRecorder()
	hospitalLocationsHandler(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d", res.Code)
	}
	var rows []HospitalLocation
	if err := json.Unmarshal(res.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || rows[0].DistanceKM > rows[1].DistanceKM {
		t.Fatalf("not sorted: %+v", rows)
	}
	if rows[0].Name != "江苏省人民医院宿迁医院" {
		t.Fatalf("nearest=%q", rows[0].Name)
	}
}

func TestAppointmentCreationPersistsBooking(t *testing.T) {
	resetPatientServices(t)
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	token := "patient-test"
	patientSessionsMu.Lock()
	patientSessions[token] = patientSession{Phone: "13800138000", DisplayName: "测试患者", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `appointments`")).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	at := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"sessionId":"s1","patientName":"测试患者","hospital":"市第一人民医院","department":"呼吸内科","appointmentAt":"` + at + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/appointments", bytes.NewBufferString(body))
	req.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: token})
	res := httptest.NewRecorder()
	appointmentsHandler(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%q", res.Code, res.Body.String())
	}
	var row Appointment
	if err := json.Unmarshal(res.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.BookingNo == "" || row.PatientPhone != "13800138000" || row.PatientName != "\u6d4b\u8bd5\u60a3\u8005" {
		t.Fatalf("row=%+v", row)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTriageReportPDFReturnsPDF(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "session_id", "patient_phone", "symptom", "risk_level", "risk_text", "department", "alternatives", "reason", "preparation", "warning", "doctor_note", "status", "handled_by", "viewed_at", "processed_at", "created_at"}).AddRow(1, "s-report", "13800138000", "发热咳嗽", "P2", "尽快就诊", "呼吸内科", "全科医学科", "呼吸系统症状", "携带病历", "高热不退及时急诊", "多饮水", "pending", "", nil, nil, now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `triage_records` WHERE session_id = ? ORDER BY created_at DESC,`triage_records`.`id` LIMIT ?")).WithArgs("s-report", 1).WillReturnRows(rows)
	resetPatientServices(t)
	token := "report-owner"
	patientSessionsMu.Lock()
	patientSessions[token] = patientSession{Phone: "13800138000", DisplayName: "Patient", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	req := httptest.NewRequest(http.MethodGet, "/api/reports/triage.pdf?sessionId=s-report", nil)
	req.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: token})
	res := httptest.NewRecorder()
	triageReportPDFHandler(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(res.Body.Bytes(), []byte("%PDF")) {
		t.Fatalf("not pdf: %q", res.Body.Bytes()[:min(12, res.Body.Len())])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentsRejectAnonymousRead(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetPatientServices(t)
	req := httptest.NewRequest(http.MethodGet, "/api/appointments?sessionId=other-session", nil)
	res := httptest.NewRecorder()
	appointmentsHandler(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTriageReportPDFRejectsAnonymousAccess(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	rows := sqlmock.NewRows([]string{"id", "session_id", "patient_phone", "symptom", "risk_level", "risk_text", "department", "alternatives", "reason", "preparation", "warning", "doctor_note", "status", "handled_by", "viewed_at", "processed_at", "created_at"}).
		AddRow(1, "private-report", "13800138000", "symptom", "P2", "risk", "department", "", "", "", "", "", "pending", "", nil, nil, time.Now())
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `triage_records` WHERE session_id = ? ORDER BY created_at DESC,`triage_records`.`id` LIMIT ?")).WithArgs("private-report", 1).WillReturnRows(rows)
	req := httptest.NewRequest(http.MethodGet, "/api/reports/triage.pdf?sessionId=private-report", nil)
	res := httptest.NewRecorder()
	triageReportPDFHandler(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentStatusRejectsAnonymousAccess(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetPatientServices(t)
	resetDoctorSessions(t)
	request := httptest.NewRequest(http.MethodPost, "/api/appointments/status", bytes.NewBufferString(`{"bookingNo":"AP001","status":"canceled"}`))
	response := httptest.NewRecorder()
	appointmentStatusHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientProfileRejectsAnonymousAccess(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetPatientServices(t)
	request := httptest.NewRequest(http.MethodGet, "/api/patient/profile", nil)
	response := httptest.NewRecorder()
	patientProfileHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientProfileRejectsInvalidValues(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetPatientServices(t)
	token := "profile-invalid"
	patientSessionsMu.Lock()
	patientSessions[token] = patientSession{Phone: "13800138000", DisplayName: "\u9648\u6653", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	rows := sqlmock.NewRows([]string{"id", "phone", "display_name"}).AddRow(1, "13800138000", "\u9648\u6653")
	mock.ExpectQuery("SELECT \\* FROM `patient_profiles` WHERE phone = \\?").WithArgs("13800138000", 1).WillReturnRows(rows)
	request := httptest.NewRequest(http.MethodPost, "/api/patient/profile", bytes.NewBufferString(`{"age":121,"heightCm":175,"weightKg":68}`))
	request.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: token})
	response := httptest.NewRecorder()
	patientProfileHandler(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientProfileUpdatePersistsHealthData(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetPatientServices(t)
	token := "profile-update"
	patientSessionsMu.Lock()
	patientSessions[token] = patientSession{Phone: "13800138000", DisplayName: "\u9648\u6653", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	initial := sqlmock.NewRows([]string{"id", "phone", "display_name"}).AddRow(1, "13800138000", "\u9648\u6653")
	mock.ExpectQuery("SELECT \\* FROM `patient_profiles` WHERE phone = \\?").WithArgs("13800138000", 1).WillReturnRows(initial)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `patient_profiles` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	updated := sqlmock.NewRows([]string{"id", "phone", "display_name", "gender", "age", "height_cm", "weight_kg", "blood_type", "chronic_diseases", "drug_allergies", "medical_history", "emergency_name", "emergency_phone", "blood_pressure", "resting_heart_rate"}).
		AddRow(1, "13800138000", "\u9648\u6653", "\u7537", 32, 175.0, 68.0, "A\u578b", "\u65e0", "\u9752\u9709\u7d20", "\u65e0", "\u9648\u5973\u58eb", "13900139000", "118/76", 72)
	mock.ExpectQuery("SELECT \\* FROM `patient_profiles` WHERE phone = \\?").WithArgs("13800138000", 1).WillReturnRows(updated)
	body := `{"phone":"19999999999","displayName":"tampered","gender":"\u7537","age":32,"heightCm":175,"weightKg":68,"bloodType":"A\u578b","chronicDiseases":"\u65e0","drugAllergies":"\u9752\u9709\u7d20","medicalHistory":"\u65e0","emergencyName":"\u9648\u5973\u58eb","emergencyPhone":"13900139000","bloodPressure":"118/76","restingHeartRate":72}`
	request := httptest.NewRequest(http.MethodPost, "/api/patient/profile", bytes.NewBufferString(body))
	request.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: token})
	response := httptest.NewRecorder()
	patientProfileHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var profile PatientProfile
	if err := json.Unmarshal(response.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Phone != "13800138000" || profile.DisplayName != "\u9648\u6653" || profile.Age != 32 || profile.EmergencyPhone != "13900139000" {
		t.Fatalf("profile=%+v", profile)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientProfileAIContextExcludesContactDetails(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	rows := sqlmock.NewRows([]string{"id", "phone", "display_name", "gender", "age", "chronic_diseases", "drug_allergies", "medical_history", "emergency_name", "emergency_phone"}).
		AddRow(1, "13800138000", "\u9648\u6653", "\u7537", 32, "\u9ad8\u8840\u538b", "\u9752\u9709\u7d20", "\u9611\u5c3e\u5207\u9664\u672f", "\u59da\u6052", "19552075183")
	mock.ExpectQuery("SELECT \\* FROM `patient_profiles` WHERE phone = \\?").WithArgs("13800138000", 1).WillReturnRows(rows)
	context := patientProfileAIContext("13800138000")
	if !strings.Contains(context, "\u9ad8\u8840\u538b") || !strings.Contains(context, "\u9752\u9709\u7d20") || !strings.Contains(context, "\u9611\u5c3e\u5207\u9664\u672f") {
		t.Fatalf("missing medical context: %q", context)
	}
	if strings.Contains(context, "\u59da\u6052") || strings.Contains(context, "19552075183") {
		t.Fatalf("contact details leaked into AI context: %q", context)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientProfileSnapshotIncludesHistoricalContact(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	rows := sqlmock.NewRows([]string{"id", "phone", "display_name", "gender", "age", "drug_allergies", "emergency_name", "emergency_phone"}).
		AddRow(1, "13800138000", "\u9648\u6653", "\u7537", 32, "\u9752\u9709\u7d20", "\u59da\u6052", "19552075183")
	mock.ExpectQuery("SELECT \\* FROM `patient_profiles` WHERE phone = \\?").WithArgs("13800138000", 1).WillReturnRows(rows)
	value := patientProfileSnapshotJSON("13800138000")
	var snapshot triagePatientProfileSnapshot
	if err := json.Unmarshal([]byte(value), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.DisplayName != "\u9648\u6653" || snapshot.DrugAllergies != "\u9752\u9709\u7d20" || snapshot.EmergencyPhone != "19552075183" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentAccessPrefersDoctorWhenBothCookiesExist(t *testing.T) {
	resetPatientServices(t)
	resetDoctorSessions(t)
	patientToken := "patient-cookie"
	patientSessionsMu.Lock()
	patientSessions[patientToken] = patientSession{Phone: "13800138000", DisplayName: "Patient", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	doctorCookie := loginDoctorForTest(t, "Doctor")
	request := httptest.NewRequest(http.MethodGet, "/api/appointments", nil)
	request.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: patientToken})
	request.AddCookie(doctorCookie)
	_, role, actor, ok := appointmentAccess(request)
	if !ok || role != "doctor" || actor != "doctor" {
		t.Fatalf("role=%q actor=%q ok=%v", role, actor, ok)
	}
}

func TestAppointmentStatusRejectsInvalidTransition(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "Doctor")
	rows := sqlmock.NewRows([]string{"id", "booking_no", "status"}).AddRow(1, "AP001", "completed")
	mock.ExpectQuery("SELECT \\* FROM `appointments` WHERE booking_no = \\?").WithArgs("AP001", 1).WillReturnRows(rows)
	request := httptest.NewRequest(http.MethodPost, "/api/appointments/status", bytes.NewBufferString(`{"bookingNo":"AP001","status":"confirmed"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	appointmentStatusHandler(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPatientVerificationAttemptsAreLimited(t *testing.T) {
	resetPatientServices(t)
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	tokenPhone := "13800138000"
	smsCodesMu.Lock()
	smsCodes[tokenPhone] = smsCode{Code: "123456", ExpiresAt: time.Now().Add(time.Minute), SentAt: time.Now().Add(-time.Minute), Attempts: 5}
	smsCodesMu.Unlock()
	request := httptest.NewRequest(http.MethodPost, "/api/patient/login", bytes.NewBufferString(`{"phone":"13800138000","code":"000000"}`))
	response := httptest.NewRecorder()
	patientLoginHandler(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
