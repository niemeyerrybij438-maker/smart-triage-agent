package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPriorityForEscalation(t *testing.T) {
	if got := priorityForEscalation("high_risk"); got != "P1" {
		t.Fatalf("high risk priority = %q, want P1", got)
	}
	if got := priorityForEscalation("low_confidence"); got != "P2" {
		t.Fatalf("low confidence priority = %q, want P2", got)
	}
}

func TestEscalationTicketsRequiresAuthentication(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	resetPatientServices(t)

	request := httptest.NewRequest(http.MethodGet, "/api/escalation-tickets", nil)
	response := httptest.NewRecorder()
	escalationTicketsHandler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database access: %v", err)
	}
}

func TestPriorityForRiskLevel(t *testing.T) {
	cases := map[string]string{"P1": "P1", "p2": "P2", " P3 ": "P3", "unknown": ""}
	for input, want := range cases {
		if got := priorityForRiskLevel(input); got != want {
			t.Fatalf("priorityForRiskLevel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSyncEscalationPriorityFromRisk(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `escalation_tickets` SET .* WHERE session_id = \\?").
		WithArgs("P2", sqlmock.AnyArg(), "session-risk").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	syncEscalationPriorityFromRisk("session-risk", "P2")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestEscalationTicketRejectsReplyBeforeAccept(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	resetDoctorSessions(t)
	cookie := loginDoctorForTest(t, "Doctor")
	rows := sqlmock.NewRows([]string{"id", "session_id", "status"}).AddRow(1, "ticket-session", "pending")
	mock.ExpectQuery("SELECT \\* FROM `escalation_tickets` WHERE session_id = \\?").WithArgs("ticket-session", 1).WillReturnRows(rows)
	request := httptest.NewRequest(http.MethodPost, "/api/escalation-tickets", bytes.NewBufferString(`{"sessionId":"ticket-session","action":"reply","reply":"done"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	escalationTicketsHandler(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
