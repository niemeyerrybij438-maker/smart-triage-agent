package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CoolBanHub/aggo/utils"
	"gorm.io/gorm"
)

type TriageRecord struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	SessionID      string     `gorm:"size:64;index" json:"sessionId"`
	PatientPhone   string     `gorm:"size:32;index" json:"patientPhone,omitempty"`
	PatientProfile string     `gorm:"type:text" json:"patientProfile,omitempty"`
	RAGEvidence    string     `gorm:"type:longtext" json:"ragEvidence,omitempty"`
	Symptom        string     `gorm:"type:text" json:"symptom"`
	RiskLevel      string     `gorm:"size:24;index" json:"riskLevel"`
	RiskText       string     `gorm:"size:64" json:"riskText"`
	Department     string     `gorm:"size:128;index" json:"department"`
	Alternatives   string     `gorm:"size:255" json:"alternatives"`
	Reason         string     `gorm:"type:text" json:"reason"`
	Preparation    string     `gorm:"type:text" json:"preparation"`
	Warning        string     `gorm:"type:text" json:"warning"`
	DoctorNote     string     `gorm:"type:text" json:"doctorNote"`
	Status         string     `gorm:"size:24;default:pending;index" json:"status"`
	HandledBy      string     `gorm:"size:128" json:"handledBy"`
	ViewedAt       *time.Time `json:"viewedAt"`
	ProcessedAt    *time.Time `json:"processedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type SaveTriageRecordRequest struct {
	SessionID      string `json:"sessionId"`
	PatientPhone   string `json:"patientPhone,omitempty"`
	PatientProfile string `json:"patientProfile,omitempty"`
	RAGEvidence    string `json:"ragEvidence,omitempty"`
	Symptom        string `json:"symptom"`
	RiskLevel      string `json:"riskLevel"`
	RiskText       string `json:"riskText"`
	Department     string `json:"department"`
	Alternatives   string `json:"alternatives"`
	Reason         string `json:"reason"`
	Preparation    string `json:"preparation"`
	Warning        string `json:"warning"`
	DoctorNote     string `json:"doctorNote"`
	Status         string `json:"status"`
}

func upsertTriageRecord(req SaveTriageRecordRequest) error {
	record := TriageRecord{
		SessionID:      strings.TrimSpace(req.SessionID),
		PatientPhone:   strings.TrimSpace(req.PatientPhone),
		PatientProfile: strings.TrimSpace(req.PatientProfile),
		RAGEvidence:    strings.TrimSpace(req.RAGEvidence),
		Symptom:        cleanSymptomText(req.Symptom),
		RiskLevel:      strings.TrimSpace(req.RiskLevel),
		RiskText:       strings.TrimSpace(req.RiskText),
		Department:     strings.TrimSpace(req.Department),
		Alternatives:   strings.TrimSpace(req.Alternatives),
		Reason:         strings.TrimSpace(req.Reason),
		Preparation:    strings.TrimSpace(req.Preparation),
		Warning:        strings.TrimSpace(req.Warning),
		DoctorNote:     strings.TrimSpace(req.DoctorNote),
		Status:         strings.TrimSpace(req.Status),
	}
	if record.SessionID == "" {
		record.SessionID = utils.GetULID()
	}
	if record.Status == "" {
		record.Status = "pending"
	}

	var existing TriageRecord
	err := globalDB.Where("session_id = ?", record.SessionID).Order("created_at DESC").First(&existing).Error
	if err == nil {
		existing.PatientPhone = record.PatientPhone
		if strings.TrimSpace(existing.PatientProfile) == "" {
			existing.PatientProfile = record.PatientProfile
		}
		if record.RAGEvidence != "" {
			existing.RAGEvidence = record.RAGEvidence
		}
		existing.Symptom = record.Symptom
		existing.RiskLevel = record.RiskLevel
		existing.RiskText = record.RiskText
		existing.Department = record.Department
		existing.Alternatives = record.Alternatives
		existing.Reason = record.Reason
		existing.Preparation = record.Preparation
		existing.Warning = record.Warning
		existing.CreatedAt = time.Now()
		if existing.Status == "" {
			existing.Status = record.Status
		}
		if err := globalDB.Save(&existing).Error; err != nil {
			return err
		}
		syncEscalationPriorityFromRisk(existing.SessionID, existing.RiskLevel)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	if err := globalDB.Create(&record).Error; err != nil {
		return err
	}
	syncEscalationPriorityFromRisk(record.SessionID, record.RiskLevel)
	return nil
}

func normalizeLegacyRAGEvidence(summary string, traceTime time.Time) (string, bool) {
	var rows []medicalKnowledgeHit
	if json.Unmarshal([]byte(strings.TrimSpace(summary)), &rows) != nil || len(rows) == 0 {
		return "", false
	}
	for index := range rows {
		if rows[index].Version < 1 {
			rows[index].Version = 1
		}
		if rows[index].RetrievedAt.IsZero() {
			rows[index].RetrievedAt = traceTime.UTC()
		}
	}
	encoded, err := json.Marshal(rows)
	return string(encoded), err == nil
}

func backfillRAGEvidenceSnapshots() error {
	if globalDB == nil {
		return nil
	}
	var records []TriageRecord
	if err := globalDB.Where("rag_evidence IS NULL OR rag_evidence = ?", "").Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		var trace AgentTrace
		if err := globalDB.Where("session_id = ? AND agent_name = ?", record.SessionID, "medical-knowledge-rag").Order("created_at DESC").First(&trace).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				continue
			}
			return err
		}
		snapshot, ok := normalizeLegacyRAGEvidence(trace.Summary, trace.CreatedAt)
		if !ok {
			continue
		}
		if err := globalDB.Model(&TriageRecord{}).Where("id = ? AND (rag_evidence IS NULL OR rag_evidence = ?)", record.ID, "").Update("rag_evidence", snapshot).Error; err != nil {
			return err
		}
	}
	return nil
}
func cleanSymptomText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimFunc(value, func(r rune) bool {
		switch r {
		case ' ', '?', ',', '.', ';', '!', 0x3001, 0x3002, 0xff0c, 0xff1b, 0xff01, 0xff1f:
			return true
		default:
			return false
		}
	})
	return strings.TrimSpace(value)
}

func cleanSymptomDisplay(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ';', '?', 0xff1f, 0xff1b:
			return true
		default:
			return false
		}
	})
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanSymptomText(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, string(rune(0xff1b)))
}

func triageRecordsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		var records []TriageRecord
		query := globalDB.Order("created_at DESC")
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		if _, doctorOK := currentDoctor(r); doctorOK {
			// Doctors always see the complete queue, even when a patient cookie exists in the same browser.
		} else if patient, patientOK := currentPatient(r); patientOK {
			query = query.Where("patient_phone = ?", patient.Phone)
			if sessionID != "" {
				query = query.Where("session_id = ?", sessionID)
			}
		} else {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := query.Limit(100).Find(&records).Error; err != nil {
			http.Error(w, "failed to query triage records", http.StatusInternalServerError)
			return
		}
		profileFallbacks := make(map[string]string)
		seen := make(map[string]bool)
		deduped := make([]TriageRecord, 0, len(records))
		for _, record := range records {
			if strings.TrimSpace(record.PatientPhone) != "" {
				currentProfile, loaded := profileFallbacks[record.PatientPhone]
				if !loaded {
					currentProfile = patientProfileSnapshotJSON(record.PatientPhone)
					profileFallbacks[record.PatientPhone] = currentProfile
				}
				record.PatientProfile = mergePatientProfileSnapshotJSON(record.PatientProfile, currentProfile)
			}
			key := strings.TrimSpace(record.SessionID) + "|" + strings.TrimSpace(record.Department)
			if key == "|" {
				key = fmt.Sprintf("id:%d", record.ID)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			record.Symptom = cleanSymptomDisplay(record.Symptom)
			deduped = append(deduped, record)
			if len(deduped) >= 50 {
				break
			}
		}
		_ = json.NewEncoder(w).Encode(deduped)
	case http.MethodPost:
		if _, ok := requireDoctorAPI(w, r); !ok {
			return
		}
		var req SaveTriageRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Symptom) == "" || strings.TrimSpace(req.Department) == "" {
			http.Error(w, "symptom and department are required", http.StatusBadRequest)
			return
		}
		record := TriageRecord{SessionID: strings.TrimSpace(req.SessionID), Symptom: cleanSymptomText(req.Symptom), RiskLevel: strings.TrimSpace(req.RiskLevel), RiskText: strings.TrimSpace(req.RiskText), Department: strings.TrimSpace(req.Department), Alternatives: strings.TrimSpace(req.Alternatives), Reason: strings.TrimSpace(req.Reason), Preparation: strings.TrimSpace(req.Preparation), Warning: strings.TrimSpace(req.Warning), DoctorNote: strings.TrimSpace(req.DoctorNote), Status: strings.TrimSpace(req.Status)}
		if record.SessionID == "" {
			record.SessionID = utils.GetULID()
		}
		if record.Status == "" {
			record.Status = "pending"
		}

		var existing TriageRecord
		err := globalDB.Where("session_id = ? AND department = ?", record.SessionID, record.Department).Order("created_at DESC").First(&existing).Error
		if err == nil {
			if !strings.Contains(existing.Symptom, record.Symptom) {
				existing.Symptom = cleanSymptomDisplay(existing.Symptom + "?" + cleanSymptomText(record.Symptom))
			}
			existing.RiskLevel = record.RiskLevel
			existing.RiskText = record.RiskText
			existing.Alternatives = record.Alternatives
			existing.Reason = record.Reason
			existing.Preparation = record.Preparation
			existing.Warning = record.Warning
			if record.DoctorNote != "" {
				existing.DoctorNote = record.DoctorNote
			}
			if record.Status != "" {
				existing.Status = record.Status
			}
			existing.CreatedAt = time.Now()
			if err := globalDB.Save(&existing).Error; err != nil {
				http.Error(w, "failed to update triage record", http.StatusInternalServerError)
				return
			}
			syncEscalationPriorityFromRisk(existing.SessionID, existing.RiskLevel)
			_ = json.NewEncoder(w).Encode(existing)
			return
		}
		if err != gorm.ErrRecordNotFound {
			http.Error(w, "failed to query triage record", http.StatusInternalServerError)
			return
		}

		if err := globalDB.Create(&record).Error; err != nil {
			http.Error(w, "failed to save triage record", http.StatusInternalServerError)
			return
		}
		syncEscalationPriorityFromRisk(record.SessionID, record.RiskLevel)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func triageRecordStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	doctorSession, ok := requireDoctorAPI(w, r)
	if !ok {
		return
	}
	var req struct {
		ID                     uint   `json:"id"`
		Status                 string `json:"status"`
		DoctorNote             string `json:"doctorNote"`
		FollowUpResolutionType string `json:"followUpResolutionType"`
		FollowUpPlanID         uint   `json:"followUpPlanId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && status != "viewed" && status != "processed" {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	resolutionType := strings.TrimSpace(req.FollowUpResolutionType)
	if status == "processed" {
		resolutionType = normalizeFollowUpResolutionType(resolutionType)
	}
	var existing TriageRecord
	if err := globalDB.First(&existing, req.ID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "triage record not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to query triage record", http.StatusInternalServerError)
		}
		return
	}
	if status != "" {
		allowed := (existing.Status == "pending" && (status == "viewed" || status == "processed")) || (existing.Status == "viewed" && status == "processed") || existing.Status == status
		if !allowed {
			http.Error(w, "invalid triage status transition", http.StatusConflict)
			return
		}
	}
	updates := map[string]any{"handled_by": doctorSession.DisplayName}
	now := time.Now()
	if status != "" {
		updates["status"] = status
		if status == "viewed" {
			updates["viewed_at"] = now
		}
		if status == "processed" {
			updates["processed_at"] = now
		}
	}
	updates["doctor_note"] = strings.TrimSpace(req.DoctorNote)
	if err := globalDB.Model(&TriageRecord{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
		http.Error(w, "failed to update status", http.StatusInternalServerError)
		return
	}
	existing.Status = status
	existing.DoctorNote = strings.TrimSpace(req.DoctorNote)
	syncEscalationFromDoctor(existing.SessionID, doctorSession.DisplayName, status, req.DoctorNote)
	if status == "processed" {
		if err := resolveEscalatedFollowUps(req.FollowUpPlanID, existing.SessionID, resolutionType, req.DoctorNote, doctorSession.DisplayName, now); err != nil {
			http.Error(w, "failed to resolve follow-up escalation", http.StatusInternalServerError)
			return
		}
		writeAudit(doctorSession.DisplayName, "follow_up_resolved", existing.SessionID, followUpResolutionLabel(resolutionType)+": "+strings.TrimSpace(req.DoctorNote))
		_ = generateFollowUpNotifications(now)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "status": status, "doctorNote": strings.TrimSpace(req.DoctorNote), "handledBy": doctorSession.DisplayName, "followUpResolutionType": resolutionType})
}
