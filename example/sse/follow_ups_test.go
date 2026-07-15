package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowUpOutcomeStatus(t *testing.T) {
	for _, test := range []struct {
		outcome string
		valid   bool
		status  string
	}{
		{outcome: "improved", valid: true, status: "completed"},
		{outcome: "unchanged", valid: true, status: "completed"},
		{outcome: "worsened", valid: true, status: "escalated"},
		{outcome: "unknown", valid: false, status: "completed"},
	} {
		if got := validFollowUpOutcome(test.outcome); got != test.valid {
			t.Fatalf("validFollowUpOutcome(%q)=%v want %v", test.outcome, got, test.valid)
		}
		if got := followUpStatusForOutcome(test.outcome); got != test.status {
			t.Fatalf("followUpStatusForOutcome(%q)=%q want %q", test.outcome, got, test.status)
		}
	}
}

func TestFollowUpResolutionTypes(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "continue_observation", want: "继续观察"},
		{value: "outpatient_review", want: "建议复诊"},
		{value: "emergency_referral", want: "转急诊处理"},
		{value: "no_further_action", want: "无需进一步处理"},
		{value: "unknown", want: "医生已处理"},
	}
	for _, test := range tests {
		if got := followUpResolutionLabel(test.value); got != test.want {
			t.Fatalf("followUpResolutionLabel(%q)=%q want %q", test.value, got, test.want)
		}
	}
}

func TestFollowUpsRequireAuthenticatedRole(t *testing.T) {
	db, _ := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	request := httptest.NewRequest(http.MethodGet, "/api/follow-ups", nil)
	response := httptest.NewRecorder()
	followUpsHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestFollowUpFeedbackRequiresPatient(t *testing.T) {
	db, _ := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	request := httptest.NewRequest(http.MethodPost, "/api/follow-ups/feedback", strings.NewReader(`{"id":1,"outcome":"improved"}`))
	response := httptest.NewRecorder()
	followUpFeedbackHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestFollowUpPagesExposeWorkflow(t *testing.T) {
	checks := []struct {
		name    string
		page    string
		markers []string
	}{
		{name: "doctor", page: string(doctorPage), markers: []string{"saveFollowUp", "/api/follow-ups", "复诊随访计划", "doctorResolutionType"}},
		{name: "patient", page: string(patientPage), markers: []string{`data-view="followups"`, "submitFollowUp", "/api/follow-ups/feedback", "doctorResolution"}},
		{name: "admin", page: string(adminPage), markers: []string{"followUpPending", "followUpEscalated", "异常升级", "doctorResolution"}},
	}
	for _, check := range checks {
		for _, marker := range check.markers {
			if !strings.Contains(check.page, marker) {
				t.Fatalf("%s page missing follow-up marker %q", check.name, marker)
			}
		}
	}
}
