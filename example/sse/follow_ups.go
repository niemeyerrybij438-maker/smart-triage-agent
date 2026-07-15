package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

type FollowUpPlan struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	TriageRecordID   uint       `gorm:"index" json:"triageRecordId"`
	SessionID        string     `gorm:"size:64;index" json:"sessionId"`
	PatientPhone     string     `gorm:"size:32;index" json:"patientPhone,omitempty"`
	Symptom          string     `gorm:"type:text" json:"symptom"`
	Department       string     `gorm:"size:128" json:"department"`
	Advice           string     `gorm:"type:text" json:"advice"`
	Precautions      string     `gorm:"type:text" json:"precautions"`
	ScheduledAt      time.Time  `gorm:"index" json:"scheduledAt"`
	Status           string     `gorm:"size:24;default:pending;index" json:"status"`
	CreatedBy        string     `gorm:"size:128" json:"createdBy"`
	PatientOutcome   string     `gorm:"size:24;index" json:"patientOutcome"`
	PatientNote      string     `gorm:"type:text" json:"patientNote"`
	RespondedAt      *time.Time `json:"respondedAt"`
	ResolutionType   string     `gorm:"size:32;index" json:"resolutionType"`
	DoctorResolution string     `gorm:"type:text" json:"doctorResolution"`
	ResolvedBy       string     `gorm:"size:128" json:"resolvedBy"`
	ResolvedAt       *time.Time `gorm:"index" json:"resolvedAt"`
	CreatedAt        time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func refreshFollowUpStatuses() {
	if globalDB == nil {
		return
	}
	now := time.Now()
	_ = globalDB.Model(&FollowUpPlan{}).
		Where("status = ? AND scheduled_at < ?", "pending", now).
		Updates(map[string]any{"status": "overdue", "updated_at": now}).Error
}

func followUpsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = maybeGenerateFollowUpNotifications(time.Now())
		query := globalDB.Order("created_at DESC").Limit(100)
		if _, doctorOK := currentDoctor(r); doctorOK {
		} else if patient, patientOK := currentPatient(r); patientOK {
			query = query.Where("patient_phone = ?", patient.Phone)
		} else if _, adminOK := currentAdminSession(r); !adminOK {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var plans []FollowUpPlan
		if err := query.Find(&plans).Error; err != nil {
			http.Error(w, "failed to query follow-ups", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plans)
	case http.MethodPost:
		doctor, ok := requireDoctorAPI(w, r)
		if !ok {
			return
		}
		var req struct {
			TriageRecordID uint   `json:"triageRecordId"`
			Advice         string `json:"advice"`
			Precautions    string `json:"precautions"`
			ScheduledAt    string `json:"scheduledAt"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.TriageRecordID == 0 {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		scheduledAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScheduledAt))
		if err != nil {
			http.Error(w, "invalid scheduled time", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Advice) == "" {
			http.Error(w, "follow-up advice is required", http.StatusBadRequest)
			return
		}
		var record TriageRecord
		if err := globalDB.First(&record, req.TriageRecordID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "triage record not found", http.StatusNotFound)
			} else {
				http.Error(w, "failed to query triage record", http.StatusInternalServerError)
			}
			return
		}
		var existing FollowUpPlan
		err = globalDB.Where("triage_record_id = ? AND status IN ?", record.ID, []string{"pending", "overdue"}).Order("created_at DESC").First(&existing).Error
		if err == nil {
			existing.Advice = strings.TrimSpace(req.Advice)
			existing.Precautions = strings.TrimSpace(req.Precautions)
			existing.ScheduledAt = scheduledAt
			existing.Status = "pending"
			existing.CreatedBy = doctor.DisplayName
			if err := globalDB.Save(&existing).Error; err != nil {
				http.Error(w, "failed to update follow-up", http.StatusInternalServerError)
				return
			}
			resetPendingFollowUpNotifications(existing.ID)
			writeAudit(doctor.DisplayName, "follow_up_update", record.SessionID, existing.Advice)
			_ = generateFollowUpNotifications(time.Now())
			_ = json.NewEncoder(w).Encode(existing)
			return
		}
		if err != gorm.ErrRecordNotFound {
			http.Error(w, "failed to query follow-up", http.StatusInternalServerError)
			return
		}
		plan := FollowUpPlan{
			TriageRecordID: record.ID,
			SessionID:      record.SessionID,
			PatientPhone:   record.PatientPhone,
			Symptom:        cleanSymptomDisplay(record.Symptom),
			Department:     record.Department,
			Advice:         strings.TrimSpace(req.Advice),
			Precautions:    strings.TrimSpace(req.Precautions),
			ScheduledAt:    scheduledAt,
			Status:         "pending",
			CreatedBy:      doctor.DisplayName,
		}
		if err := globalDB.Create(&plan).Error; err != nil {
			http.Error(w, "failed to create follow-up", http.StatusInternalServerError)
			return
		}
		writeAudit(doctor.DisplayName, "follow_up_create", record.SessionID, plan.Advice)
		_ = generateFollowUpNotifications(time.Now())
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(plan)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func followUpFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	patient, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID      uint   `json:"id"`
		Outcome string `json:"outcome"`
		Note    string `json:"note"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == 0 || !validFollowUpOutcome(req.Outcome) {
		http.Error(w, "invalid follow-up feedback", http.StatusBadRequest)
		return
	}
	var plan FollowUpPlan
	if err := globalDB.Where("id = ? AND patient_phone = ?", req.ID, patient.Phone).First(&plan).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "follow-up not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to query follow-up", http.StatusInternalServerError)
		}
		return
	}
	if plan.Status == "completed" || plan.Status == "escalated" {
		http.Error(w, "follow-up feedback already submitted", http.StatusConflict)
		return
	}
	now := time.Now()
	outcome := strings.TrimSpace(req.Outcome)
	status := followUpStatusForOutcome(outcome)
	updates := map[string]any{
		"patient_outcome": outcome,
		"patient_note":    strings.TrimSpace(req.Note),
		"responded_at":    now,
		"status":          status,
		"updated_at":      now,
	}
	if err := globalDB.Model(&plan).Updates(updates).Error; err != nil {
		http.Error(w, "failed to save follow-up feedback", http.StatusInternalServerError)
		return
	}
	completePatientFollowUpNotifications(plan.ID, now)
	if outcome == "worsened" {
		ensureEscalationTicket(plan.SessionID, patient.Phone, "follow_up_worsened")
		writeAudit(patient.DisplayName, "follow_up_escalated", plan.SessionID, strings.TrimSpace(req.Note))
	} else {
		writeAudit(patient.DisplayName, "follow_up_feedback", plan.SessionID, outcome)
	}
	_ = generateFollowUpNotifications(now)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": plan.ID, "status": status, "outcome": outcome, "respondedAt": now})
}

func normalizeFollowUpResolutionType(value string) string {
	switch strings.TrimSpace(value) {
	case "continue_observation", "outpatient_review", "emergency_referral", "no_further_action":
		return strings.TrimSpace(value)
	default:
		return "doctor_handled"
	}
}

func followUpResolutionLabel(value string) string {
	switch normalizeFollowUpResolutionType(value) {
	case "continue_observation":
		return "继续观察"
	case "outpatient_review":
		return "建议复诊"
	case "emergency_referral":
		return "转急诊处理"
	case "no_further_action":
		return "无需进一步处理"
	default:
		return "医生已处理"
	}
}

func followUpResolutionText(resolutionType, resolution string) string {
	if value := strings.TrimSpace(resolution); value != "" {
		return value
	}
	return followUpResolutionLabel(resolutionType)
}

func resolveEscalatedFollowUps(planID uint, sessionID, resolutionType, resolution, resolvedBy string, now time.Time) error {
	if globalDB == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	resolutionType = normalizeFollowUpResolutionType(resolutionType)
	resolution = followUpResolutionText(resolutionType, resolution)
	return globalDB.Transaction(func(tx *gorm.DB) error {
		planScope := tx.Model(&FollowUpPlan{}).Where("session_id = ?", strings.TrimSpace(sessionID))
		if planID > 0 {
			planScope = planScope.Where("id = ? AND (status = ? OR (status = ? AND patient_outcome = ?))", planID, "escalated", "completed", "worsened")
		} else {
			planScope = planScope.Where("status = ?", "escalated")
		}
		if err := tx.Model(&FollowUpNotification{}).
			Where("session_id = ? AND recipient_type = ? AND is_read = ?", strings.TrimSpace(sessionID), followUpRecipientDoctor, false).
			Updates(map[string]any{"is_read": true, "read_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		return planScope.Updates(map[string]any{"status": "completed", "resolution_type": resolutionType, "doctor_resolution": resolution, "resolved_by": strings.TrimSpace(resolvedBy), "resolved_at": now, "updated_at": now}).Error
	})
}

func backfillResolvedFollowUpDetails(sessionID, resolution, resolvedBy string, resolvedAt time.Time) error {
	if globalDB == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return globalDB.Model(&FollowUpPlan{}).
		Where("session_id = ? AND status = ? AND patient_outcome = ? AND resolved_at IS NULL", strings.TrimSpace(sessionID), "completed", "worsened").
		Updates(map[string]any{"resolution_type": "doctor_handled", "doctor_resolution": followUpResolutionText("doctor_handled", resolution), "resolved_by": strings.TrimSpace(resolvedBy), "resolved_at": resolvedAt, "updated_at": resolvedAt}).Error
}

func reconcileClosedEscalationFollowUps() error {
	if globalDB == nil {
		return nil
	}
	var sessionIDs []string
	if err := globalDB.Model(&EscalationTicket{}).
		Where("status = ? AND reason = ?", "closed", "follow_up_worsened").
		Distinct().Pluck("session_id", &sessionIDs).Error; err != nil {
		return err
	}
	for _, sessionID := range sessionIDs {
		var record TriageRecord
		if err := globalDB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&record).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				continue
			}
			return err
		}
		resolvedAt := time.Now()
		if record.ProcessedAt != nil {
			resolvedAt = *record.ProcessedAt
		}
		if err := resolveEscalatedFollowUps(0, sessionID, "doctor_handled", record.DoctorNote, record.HandledBy, resolvedAt); err != nil {
			return err
		}
		if err := backfillResolvedFollowUpDetails(sessionID, record.DoctorNote, record.HandledBy, resolvedAt); err != nil {
			return err
		}
	}
	return nil
}

func validFollowUpOutcome(value string) bool {
	switch strings.TrimSpace(value) {
	case "improved", "unchanged", "worsened":
		return true
	default:
		return false
	}
}

func followUpStatusForOutcome(value string) string {
	if strings.TrimSpace(value) == "worsened" {
		return "escalated"
	}
	return "completed"
}
