package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type EscalationTicket struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	SessionID        string     `gorm:"size:64;uniqueIndex" json:"sessionId"`
	PatientPhone     string     `gorm:"size:32;index" json:"patientPhone"`
	Reason           string     `gorm:"size:64;index" json:"reason"`
	Priority         string     `gorm:"size:16;index" json:"priority"`
	Status           string     `gorm:"size:24;default:pending;index" json:"status"`
	AssignedDoctor   string     `gorm:"size:128" json:"assignedDoctor"`
	DoctorReply      string     `gorm:"type:text" json:"doctorReply"`
	AcceptedAt       *time.Time `json:"acceptedAt"`
	RepliedAt        *time.Time `json:"repliedAt"`
	ClosedAt         *time.Time `json:"closedAt"`
	SLADeadline      *time.Time `gorm:"index" json:"slaDeadline"`
	Overdue          bool       `gorm:"default:false;index" json:"overdue"`
	EscalationCount  int        `gorm:"default:0" json:"escalationCount"`
	LastEscalatedAt  *time.Time `json:"lastEscalatedAt"`
	EscalationReason string     `gorm:"size:128" json:"escalationReason"`
	CreatedAt        time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func slaDurationForPriority(priority string) time.Duration {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "P1":
		return 10 * time.Minute
	case "P2":
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

func slaDeadline(createdAt time.Time, priority string) time.Time {
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return createdAt.Add(slaDurationForPriority(priority))
}

func refreshEscalationSLAs() {
	if globalDB == nil {
		return
	}
	var tickets []EscalationTicket
	if globalDB.Where("status <> ?", "closed").Find(&tickets).Error != nil {
		return
	}
	now := time.Now()
	for _, ticket := range tickets {
		updates := map[string]any{}
		deadline := ticket.SLADeadline
		if deadline == nil {
			value := slaDeadline(ticket.CreatedAt, ticket.Priority)
			deadline = &value
			updates["sla_deadline"] = value
		}
		if deadline != nil && now.After(*deadline) && !ticket.Overdue {
			updates["overdue"] = true
			updates["escalation_count"] = ticket.EscalationCount + 1
			updates["last_escalated_at"] = now
			updates["escalation_reason"] = "sla_overdue"
		}
		if len(updates) > 0 {
			_ = globalDB.Model(&ticket).Updates(updates).Error
		}
	}
}

func priorityForEscalation(reason string) string {
	if reason == "high_risk" {
		return "P1"
	}
	return "P2"
}

func priorityForRiskLevel(riskLevel string) string {
	switch strings.ToUpper(strings.TrimSpace(riskLevel)) {
	case "P1":
		return "P1"
	case "P2":
		return "P2"
	case "P3":
		return "P3"
	default:
		return ""
	}
}

func syncEscalationPriorityFromRisk(sessionID, riskLevel string) {
	if globalDB == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	priority := priorityForRiskLevel(riskLevel)
	if priority == "" {
		return
	}
	_ = globalDB.Model(&EscalationTicket{}).Where("session_id = ?", strings.TrimSpace(sessionID)).Update("priority", priority).Error
}

func reconcileEscalationPriorities() {
	if globalDB == nil {
		return
	}
	var records []TriageRecord
	if globalDB.Order("created_at DESC").Find(&records).Error != nil {
		return
	}
	seen := make(map[string]bool)
	for _, record := range records {
		sessionID := strings.TrimSpace(record.SessionID)
		if sessionID == "" || seen[sessionID] {
			continue
		}
		seen[sessionID] = true
		syncEscalationPriorityFromRisk(sessionID, record.RiskLevel)
	}
}

func ensureEscalationTicket(sessionID, phone, reason string) {
	if globalDB == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	var ticket EscalationTicket
	err := globalDB.Where("session_id = ?", sessionID).First(&ticket).Error
	if err == nil {
		priority := priorityForEscalation(reason)
		updates := map[string]any{"reason": reason, "priority": priority}
		if ticket.SLADeadline == nil {
			updates["sla_deadline"] = slaDeadline(ticket.CreatedAt, priority)
		}
		if ticket.Status == "closed" {
			updates["status"] = "pending"
			updates["closed_at"] = nil
		}
		_ = globalDB.Model(&ticket).Updates(updates).Error
		return
	}
	priority := priorityForEscalation(reason)
	createdAt := time.Now()
	deadline := slaDeadline(createdAt, priority)
	_ = globalDB.Create(&EscalationTicket{SessionID: sessionID, PatientPhone: phone, Reason: reason, Priority: priority, Status: "pending", SLADeadline: &deadline, CreatedAt: createdAt}).Error
}

func syncEscalationFromDoctor(sessionID, doctor, status, reply string) {
	if globalDB == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	var ticket EscalationTicket
	if globalDB.Where("session_id = ?", sessionID).First(&ticket).Error != nil {
		return
	}
	updates := map[string]any{"assigned_doctor": doctor}
	now := time.Now()
	if ticket.AcceptedAt == nil {
		updates["accepted_at"] = now
	}
	if strings.TrimSpace(reply) != "" {
		updates["doctor_reply"] = strings.TrimSpace(reply)
		updates["replied_at"] = now
		updates["status"] = "replied"
	}
	if status == "processed" {
		updates["status"] = "closed"
		updates["closed_at"] = now
	}
	_ = globalDB.Model(&ticket).Updates(updates).Error
}

func escalationTicketsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if doctor, ok := currentDoctorSession(r); ok {
		refreshEscalationSLAs()
		switch r.Method {
		case http.MethodGet:
			var tickets []EscalationTicket
			query := globalDB.Order("FIELD(priority, 'P1', 'P2', 'P3'), created_at DESC")
			if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
				query = query.Where("status = ?", status)
			}
			if err := query.Limit(100).Find(&tickets).Error; err != nil {
				http.Error(w, "failed to query tickets", 500)
				return
			}
			_ = json.NewEncoder(w).Encode(tickets)
		case http.MethodPost:
			var req struct {
				SessionID string `json:"sessionId"`
				Action    string `json:"action"`
				Reply     string `json:"reply"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.SessionID) == "" {
				http.Error(w, "invalid json", 400)
				return
			}
			var ticket EscalationTicket
			if globalDB.Where("session_id = ?", strings.TrimSpace(req.SessionID)).First(&ticket).Error != nil {
				http.Error(w, "ticket not found", 404)
				return
			}
			now := time.Now()
			updates := map[string]any{"assigned_doctor": doctor.DisplayName}
			reply := strings.TrimSpace(req.Reply)
			switch req.Action {
			case "accept":
				if ticket.Status != "pending" {
					http.Error(w, "ticket cannot be accepted", http.StatusConflict)
					return
				}
				updates["status"] = "accepted"
				updates["accepted_at"] = now
			case "reply":
				if ticket.Status != "accepted" || reply == "" {
					http.Error(w, "ticket must be accepted before reply", http.StatusConflict)
					return
				}
				updates["status"] = "replied"
				updates["doctor_reply"] = reply
				updates["replied_at"] = now
			case "close":
				if ticket.Status != "accepted" && ticket.Status != "replied" {
					http.Error(w, "ticket cannot be closed", http.StatusConflict)
					return
				}
				updates["status"] = "closed"
				updates["doctor_reply"] = reply
				updates["replied_at"] = now
				updates["closed_at"] = now
			default:
				http.Error(w, "invalid action", 400)
				return
			}
			if globalDB.Model(&ticket).Updates(updates).Error != nil {
				http.Error(w, "update failed", 500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "method not allowed", 405)
		}
		return
	}
	if _, ok := currentAdminSession(r); ok {
		refreshEscalationSLAs()
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var tickets []EscalationTicket
		query := globalDB.Order("FIELD(priority, 'P1', 'P2', 'P3'), created_at DESC")
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			query = query.Where("status = ?", status)
		}
		if priority := strings.TrimSpace(r.URL.Query().Get("priority")); priority != "" {
			query = query.Where("priority = ?", priority)
		}
		if err := query.Limit(200).Find(&tickets).Error; err != nil {
			http.Error(w, "failed to query tickets", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(tickets)
		return
	}
	patient, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	refreshEscalationSLAs()
	var tickets []EscalationTicket
	query := globalDB.Where("patient_phone = ?", patient.Phone).Order("created_at DESC")
	if sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId")); sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}
	if err := query.Limit(50).Find(&tickets).Error; err != nil {
		http.Error(w, "failed to query tickets", 500)
		return
	}
	_ = json.NewEncoder(w).Encode(tickets)
}
