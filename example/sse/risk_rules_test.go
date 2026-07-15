package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAdminRiskRulesRequireAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/risk-rules", nil)
	res := httptest.NewRecorder()
	adminRiskRulesHandler(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.Code)
	}
}
func TestAdminRiskRuleCreate(t *testing.T) {
	resetAdminSessions(t)
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	cookie := adminCookieForTest(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `risk_rules`")).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `audit_logs`")).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/risk-rules", bytes.NewBufferString(`{"code":"P2-FEVER","name":"持续高热","description":"高热不退","level":"P2","keywords":"高热"}`))
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	adminRiskRulesHandler(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%q", res.Code, res.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
