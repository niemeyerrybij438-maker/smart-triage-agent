package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaskFollowUpPhone(t *testing.T) {
	if got := maskFollowUpPhone("19552075183"); got != "195****5183" {
		t.Fatalf("masked phone = %q", got)
	}
	if got := maskFollowUpPhone("12345"); got != "12345" {
		t.Fatalf("short value = %q", got)
	}
}

func TestCSVSafePreventsSpreadsheetFormula(t *testing.T) {
	for _, value := range []string{"=cmd", "+sum", "-1+2", "@value"} {
		if got := csvSafe(value); !strings.HasPrefix(got, "'") {
			t.Fatalf("csvSafe(%q) = %q", value, got)
		}
	}
	if got := csvSafe("normal"); got != "normal" {
		t.Fatalf("normal value = %q", got)
	}
}

func TestAdminFollowUpExportRequiresAdmin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/admin/follow-ups/export?type=plans", nil)
	response := httptest.NewRecorder()
	adminFollowUpExportHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestParseAdminFollowUpFiltersRejectsReversedDates(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/admin/follow-ups?from=2026-07-15&to=2026-07-14", nil)
	if _, err := parseAdminFollowUpFilters(request); err == nil {
		t.Fatal("expected reversed date range error")
	}
}
