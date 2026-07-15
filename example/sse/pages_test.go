package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIndexHandlerRequiresPatientLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	indexHandler(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/patient/login" {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func TestIndexHandlerServesPatientAfterLogin(t *testing.T) {
	token := "patient-page-test"
	patientSessionsMu.Lock()
	patientSessions[token] = patientSession{Phone: "13800138000", DisplayName: "Patient", ExpiresAt: time.Now().Add(time.Hour)}
	patientSessionsMu.Unlock()
	t.Cleanup(func() { patientSessionsMu.Lock(); delete(patientSessions, token); patientSessionsMu.Unlock() })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: patientSessionCookie, Value: token})
	response := httptest.NewRecorder()
	indexHandler(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != len(patientPage) {
		t.Fatalf("status=%d length=%d", response.Code, response.Body.Len())
	}
}

func TestPatientLoginPageServesEmbeddedPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/patient/login", nil)
	response := httptest.NewRecorder()
	patientLoginPageHandler(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != len(patientLoginPage) {
		t.Fatalf("status=%d length=%d", response.Code, response.Body.Len())
	}
}

func TestDoctorLoginPageHandlerServesEmbeddedPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/doctor/login", nil)
	response := httptest.NewRecorder()

	doctorLoginPageHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != len(doctorLoginPage) {
		t.Fatalf("body length = %d, want %d", response.Body.Len(), len(doctorLoginPage))
	}
}

func TestDoctorHandlerRequiresLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/doctor", nil)
	response := httptest.NewRecorder()

	doctorHandler(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/doctor/login" {
		t.Fatalf("redirect location = %q, want %q", location, "/doctor/login")
	}
}

func TestDoctorHandlerServesEmbeddedPageAfterLogin(t *testing.T) {
	token := "test-doctor-session"
	doctorSessionsMu.Lock()
	doctorSessions[token] = doctorSession{Username: "doctor", DisplayName: "Doctor", ExpiresAt: time.Now().Add(time.Hour)}
	doctorSessionsMu.Unlock()
	t.Cleanup(func() {
		doctorSessionsMu.Lock()
		delete(doctorSessions, token)
		doctorSessionsMu.Unlock()
	})

	request := httptest.NewRequest(http.MethodGet, "/doctor", nil)
	request.AddCookie(&http.Cookie{Name: doctorSessionCookie, Value: token})
	response := httptest.NewRecorder()

	doctorHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != len(doctorPage) {
		t.Fatalf("body length = %d, want %d", response.Body.Len(), len(doctorPage))
	}
}

func TestPatientReportListLoadsAllAuthenticatedRecords(t *testing.T) {
	page := string(patientPage)
	if !strings.Contains(page, "patientAPI('/api/triage-records')") {
		t.Fatal("patient report history must load all records for the authenticated patient")
	}
	if strings.Contains(page, "fetch('/api/triage-records?sessionId='+encodeURIComponent(sessionId))") {
		t.Fatal("patient report history must not be restricted to the active conversation")
	}
}

func TestDoctorRiskReportIncludesHistorySelector(t *testing.T) {
	page := string(doctorPage)
	for _, marker := range []string{`id="reportQueue"`, `id="reportTotal"`, "bindRecordItems($('#reportQueue'))", `data-view="records"`, `id="view-records"`, `id="archiveQueue"`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("doctor report history selector missing %q", marker)
		}
	}
}

func TestDoctorDashboardAndPatientQueueAreSeparateViews(t *testing.T) {
	page := string(doctorPage)
	for _, marker := range []string{`id="view-dashboard"`, `data-view="patients"`, `id="view-patients"`, `id="overviewPending"`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("doctor dashboard separation missing %q", marker)
		}
	}
	if strings.Count(page, `id="queue"`) != 1 {
		t.Fatalf("patient queue should exist once, got %d", strings.Count(page, `id="queue"`))
	}
}

func TestDoctorPatientQueueHasSearchFiltersAndPagination(t *testing.T) {
	page := string(doctorPage)
	for _, marker := range []string{`id="patientKeyword"`, `id="patientRisk"`, `id="patientStatus"`, `id="patientPrev"`, `id="patientNext"`, `function patientFilteredRecords()`, `function renderPatientQueue()`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("doctor patient queue controls missing %q", marker)
		}
	}
}

func TestPatientReportHistoryHasFiltersSortingAndPagination(t *testing.T) {
	page := string(patientPage)
	for _, marker := range []string{`id="reportKeyword"`, `id="reportRisk"`, `id="reportMonth"`, `id="reportSort"`, `id="reportPrev"`, `id="reportNext"`, `function reportFilteredRows()`, `function renderPatientReports()`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("patient report controls missing %q", marker)
		}
	}
}

func TestAdminAnalysisHasRecordFiltersSortingAndPagination(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="analysisKeyword"`, `id="analysisRisk"`, `id="analysisStatus"`, `id="analysisMonth"`, `id="analysisSort"`, `id="analysisPrev"`, `id="analysisNext"`, `function filteredAnalysisRecords()`, `function renderAnalysisRecords()`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin analysis controls missing %q", marker)
		}
	}
}

func TestAdminAgentTraceHasFiltersPaginationAndDetail(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="traceKeyword"`, `id="traceAgentFilter"`, `id="traceStatusFilter"`, `id="traceSort"`, `id="traceGroupFilter"`, `id="tracePrev"`, `id="traceNext"`, `id="traceDetail"`, `function filteredTraces()`, `function renderTraceDetail(`, `id="traceSessionFilter"`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin agent trace controls missing %q", marker)
		}
	}
}

func TestAdminEscalationTicketsHavePaginationPersistenceAndLinks(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="resetTicketFilters"`, `id="ticketPrev"`, `id="ticketNext"`, `id="ticketSummary"`, `function filteredTickets()`, `function goTicketReport(`, `function goTicketTrace(`, `ticket-report`, `ticket-trace`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin ticket controls missing %q", marker)
		}
	}
}

func TestAdminAppointmentsHaveFiltersPaginationAndReportLink(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="appointmentSearch"`, `id="appointmentStatusFilter"`, `id="appointmentMonthFilter"`, `id="appointmentPrev"`, `id="appointmentNext"`, `id="appointmentTotal"`, `function filteredAppointments()`, `function goAppointmentReport(`, `appointment-report`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin appointment controls missing %q", marker)
		}
	}
}

func TestAdminPlatformConfigUsesPersistedAPI(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="platformConfigForm"`, `id="configServiceName"`, `id="configRetentionDays"`, `loadPlatformConfig()`, `/api/admin/platform-config`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin platform config missing %q", marker)
		}
	}
	if strings.Contains(page, "\u4fdd\u5b58\u914d\u7f6e\uff08\u6f14\u793a\uff09") {
		t.Fatal("platform config must not use a demo-only save button")
	}
}

func TestAdminPermissionsHaveFiltersPaginationAndPasswordReset(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="userDepartment"`, `id="userSort"`, `id="userPrev"`, `id="userNext"`, `id="resetDoctorModal"`, `id="resetDoctorForm"`, `function userRowsFiltered()`, `/api/admin/doctors/reset-password`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin permission controls missing %q", marker)
		}
	}
}

func TestAdminKnowledgeRefreshHasVisibleFeedback(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="knowledgeRefreshHint"`, `loadMedicalKnowledge(true)`, `Date.now()`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin page missing knowledge refresh marker %q", marker)
		}
	}
}

func TestAdminKnowledgeManagementHasFiltersAndEditor(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{`id="knowledgeSearch"`, `id="knowledgeStatusFilter"`, `id="knowledgeEnabledFilter"`, `id="addKnowledge"`, `id="knowledgeModal"`, `id="knowledgeForm"`, `function filteredKnowledge()`, `function renderMedicalKnowledge()`, `function openKnowledgeModal(`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin knowledge management missing %q", marker)
		}
	}
}

func TestRAGEvidenceAppearsInAllOperationalViews(t *testing.T) {
	checks := []struct {
		name    string
		page    string
		markers []string
	}{
		{name: "doctor", page: string(doctorPage), markers: []string{"function parseRAGEvidence(", "function doctorEvidenceSection(", "AI \\u53c2\\u8003\\u4f9d\\u636e"}},
		{name: "patient", page: string(patientPage), markers: []string{`id="patientEvidence"`, "function patientEvidenceHTML(", "ragEvidence"}},
		{name: "admin", page: string(adminPage), markers: []string{"function ragTraceDetail(", "medical-knowledge-rag", "RAG \\u8bc4\\u4f30"}},
	}
	for _, check := range checks {
		for _, marker := range check.markers {
			if !strings.Contains(check.page, marker) {
				t.Fatalf("%s page missing RAG evidence marker %q", check.name, marker)
			}
		}
	}
}

func TestAdminTraceFiltersRecoverFromConflictingSessionAndAgent(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{"function normalizeTraceFilterCombination()", "traceAgentFilter.onchange=()=>", "medical-knowledge-rag':'Medical Knowledge RAG", "approved:'\\u5df2\\u901a\\u8fc7'", "'approved','approved_rules','corrected'"} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin trace recovery missing %q", marker)
		}
	}
}

func TestAdminRAGTraceShowsRetrievalContext(t *testing.T) {
	page := string(adminPage)
	for _, marker := range []string{"currentInput:data.currentInput", "contextSummary:data.contextSummary", "RAG \\u68c0\\u7d22\\u4e0a\\u4e0b\\u6587", "\\u672c\\u8f6e\\u8f93\\u5165"} {
		if !strings.Contains(page, marker) {
			t.Fatalf("admin RAG context audit missing %q", marker)
		}
	}
}
