package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open mock gorm database: %v", err)
	}
	return db, mock
}

func useGlobalDBForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	previous := globalDB
	globalDB = db
	t.Cleanup(func() { globalDB = previous })
}

func TestTriageRecordsAPIFullListRequiresDoctor(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)

	request := httptest.NewRequest(http.MethodGet, "/api/triage-records", nil)
	response := httptest.NewRecorder()
	triageRecordsHandler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database access: %v", err)
	}
}

func TestTriageRecordsAPIPostRequiresDoctor(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)

	request := httptest.NewRequest(http.MethodPost, "/api/triage-records", bytes.NewBufferString(`{"symptom":"cough","department":"respiratory"}`))
	response := httptest.NewRecorder()
	triageRecordsHandler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database access: %v", err)
	}
}

func TestTriageRecordStatusPersistsAuditAndNote(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "\u674e\u533b\u751f")

	row := sqlmock.NewRows([]string{"id", "session_id", "status"}).AddRow(7, "session-7", "viewed")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `triage_records` WHERE `triage_records`.`id` = ? ORDER BY `triage_records`.`id` LIMIT ?")).WithArgs(7, 1).WillReturnRows(row)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `triage_records` SET `doctor_note`=?,`handled_by`=?,`processed_at`=?,`status`=? WHERE id = ?")).
		WithArgs("follow up tomorrow", "\u674e\u533b\u751f", sqlmock.AnyArg(), "processed", 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `follow_up_notifications`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `follow_up_plans`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := bytes.NewBufferString(`{"id":7,"status":"processed","doctorNote":" follow up tomorrow "}`)
	request := httptest.NewRequest(http.MethodPost, "/api/triage-records/status", body)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	triageRecordStatusHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"handledBy":"\u674e\u533b\u751f"`)) && !bytes.Contains(response.Body.Bytes(), []byte("\u674e\u533b\u751f")) {
		t.Fatalf("response does not include doctor audit name: %q", response.Body.String())
	}
}

func TestTriageRecordStatusRejectsInvalidStatusBeforeDatabaseWrite(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "Doctor")

	request := httptest.NewRequest(http.MethodPost, "/api/triage-records/status", bytes.NewBufferString(`{"id":7,"status":"deleted"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	triageRecordStatusHandler(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database write: %v", err)
	}
}

func TestTriageRecordsRejectAnonymousSessionLookup(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	resetPatientServices(t)
	req := httptest.NewRequest(http.MethodGet, "/api/triage-records?sessionId=other-session", nil)
	res := httptest.NewRecorder()
	triageRecordsHandler(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertTriageRecordPreservesExistingProfileSnapshot(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	existing := sqlmock.NewRows([]string{"id", "session_id", "patient_phone", "patient_profile", "symptom", "status", "created_at"}).
		AddRow(7, "snapshot-session", "13800138000", `{"age":30}`, "old symptom", "pending", time.Now())
	mock.ExpectQuery("SELECT \\* FROM `triage_records` WHERE session_id = \\?").WithArgs("snapshot-session", 1).WillReturnRows(existing)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `triage_records` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `escalation_tickets` SET").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	err := upsertTriageRecord(SaveTriageRecordRequest{SessionID: "snapshot-session", PatientPhone: "13800138000", PatientProfile: `{"age":40}`, Symptom: "new symptom", RiskLevel: "P3", Department: "general", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTriageRecordStatusRejectsRollback(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "Doctor")
	row := sqlmock.NewRows([]string{"id", "session_id", "status"}).AddRow(7, "session-7", "processed")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `triage_records` WHERE `triage_records`.`id` = ? ORDER BY `triage_records`.`id` LIMIT ?")).WithArgs(7, 1).WillReturnRows(row)
	request := httptest.NewRequest(http.MethodPost, "/api/triage-records/status", bytes.NewBufferString(`{"id":7,"status":"viewed"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	triageRecordStatusHandler(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
