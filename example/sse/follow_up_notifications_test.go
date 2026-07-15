package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFollowUpNotificationReadColumnAvoidsReservedWord(t *testing.T) {
	field, ok := reflect.TypeOf(FollowUpNotification{}).FieldByName("Read")
	if !ok {
		t.Fatal("Read field missing")
	}
	if !strings.Contains(field.Tag.Get("gorm"), "column:is_read") {
		t.Fatalf("Read gorm tag = %q", field.Tag.Get("gorm"))
	}
}
func ptrTime(value time.Time) *time.Time { return &value }

func TestFollowUpNotificationTargets(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.Local)
	tests := []struct {
		name  string
		plan  FollowUpPlan
		kinds []string
	}{
		{name: "due soon", plan: FollowUpPlan{Status: "pending", PatientPhone: "13800000000", ScheduledAt: now.Add(23 * time.Hour)}, kinds: []string{followUpNoticeDueSoon}},
		{name: "not due yet", plan: FollowUpPlan{Status: "pending", PatientPhone: "13800000000", ScheduledAt: now.Add(25 * time.Hour)}},
		{name: "overdue reaches patient and doctor", plan: FollowUpPlan{Status: "overdue", PatientPhone: "13800000000"}, kinds: []string{followUpNoticeOverdue, followUpNoticeOverdue}},
		{name: "worsened reaches doctor", plan: FollowUpPlan{Status: "escalated", PatientPhone: "13800000000"}, kinds: []string{followUpNoticeWorsened}},
		{name: "completed has no reminder", plan: FollowUpPlan{Status: "completed", PatientPhone: "13800000000"}},
		{name: "resolved worsening reaches patient", plan: FollowUpPlan{Status: "completed", PatientPhone: "13800000000", PatientOutcome: "worsened", ResolvedAt: ptrTime(now)}, kinds: []string{followUpNoticeResolved}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targets := followUpNotificationTargets(test.plan, now)
			if len(targets) != len(test.kinds) {
				t.Fatalf("target count = %d, want %d", len(targets), len(test.kinds))
			}
			for index, kind := range test.kinds {
				if targets[index].Kind != kind {
					t.Fatalf("target %d kind = %q, want %q", index, targets[index].Kind, kind)
				}
			}
		})
	}
}

func TestFollowUpNotificationCopy(t *testing.T) {
	plan := FollowUpPlan{Department: "General", ScheduledAt: time.Date(2026, 7, 15, 9, 30, 0, 0, time.Local)}
	for _, kind := range []string{followUpNoticeDueSoon, followUpNoticeOverdue, followUpNoticeWorsened, followUpNoticeResolved} {
		title, content := followUpNotificationCopy(plan, kind)
		if strings.TrimSpace(title) == "" || strings.TrimSpace(content) == "" {
			t.Fatalf("kind %q returned empty copy", kind)
		}
	}
}

func TestAdminFollowUpDetailsRequiresAdmin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/admin/follow-ups", nil)
	response := httptest.NewRecorder()
	adminFollowUpDetailsHandler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestFollowUpNotificationPageBindings(t *testing.T) {
	checks := []struct {
		name string
		page string
		ids  []string
	}{
		{name: "patient", page: string(patientPage), ids: []string{"patientNoticeButton", "patientNoticePanel", "patientNoticeAPI", "/api/follow-up-notifications"}},
		{name: "doctor", page: string(doctorPage), ids: []string{"doctorNoticeButton", "doctorNoticePanel", "/api/follow-up-notifications"}},
		{name: "admin", page: string(adminPage), ids: []string{"patientReminderUnread", "doctorReminderUnread", "remindersToday", "view-followups", "adminFollowUpPlanList", "/api/admin/follow-ups"}},
	}
	for _, check := range checks {
		for _, marker := range check.ids {
			if !strings.Contains(check.page, marker) {
				t.Fatalf("%s page missing marker %q", check.name, marker)
			}
		}
	}
}
