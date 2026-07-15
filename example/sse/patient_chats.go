package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type PatientChat struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	PatientPhone string    `gorm:"size:32;uniqueIndex:idx_patient_chat" json:"-"`
	ChatKey      string    `gorm:"size:96;uniqueIndex:idx_patient_chat" json:"key"`
	SessionID    string    `gorm:"size:64;index" json:"sessionId"`
	Title        string    `gorm:"size:255" json:"title"`
	MessagesJSON string    `gorm:"type:longtext" json:"-"`
	Progress     int       `gorm:"default:1" json:"progress"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `gorm:"index" json:"updatedAt"`
}

type patientChatPayload struct {
	Key       string            `json:"key"`
	SessionID string            `json:"sessionId"`
	Title     string            `json:"title"`
	Messages  []json.RawMessage `json:"messages"`
	Progress  int               `json:"progress"`
	UpdatedAt int64             `json:"updatedAt"`
}

func patientChatsHandler(w http.ResponseWriter, r *http.Request) {
	patient, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		var rows []PatientChat
		if err := globalDB.Where("patient_phone = ?", patient.Phone).Order("updated_at DESC").Limit(50).Find(&rows).Error; err != nil {
			http.Error(w, "failed to query chats", 500)
			return
		}
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			var messages []json.RawMessage
			if json.Unmarshal([]byte(row.MessagesJSON), &messages) != nil {
				messages = []json.RawMessage{}
			}
			items = append(items, map[string]any{"key": row.ChatKey, "sessionId": row.SessionID, "title": row.Title, "messages": messages, "progress": row.Progress, "updatedAt": row.UpdatedAt.UnixMilli()})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case http.MethodPost:
		var payload patientChatPayload
		if json.NewDecoder(r.Body).Decode(&payload) != nil || strings.TrimSpace(payload.Key) == "" {
			http.Error(w, "invalid json", 400)
			return
		}
		encoded, err := json.Marshal(payload.Messages)
		if err != nil {
			http.Error(w, "invalid messages", 400)
			return
		}
		row := PatientChat{PatientPhone: patient.Phone, ChatKey: strings.TrimSpace(payload.Key), SessionID: strings.TrimSpace(payload.SessionID), Title: strings.TrimSpace(payload.Title), MessagesJSON: string(encoded), Progress: payload.Progress}
		if row.Progress < 1 || row.Progress > 4 {
			row.Progress = 1
		}
		if row.Title == "" {
			row.Title = "\u5bfc\u8bca\u5bf9\u8bdd"
		}
		var existing PatientChat
		result := globalDB.Where("patient_phone = ? AND chat_key = ?", patient.Phone, row.ChatKey).First(&existing)
		if result.Error == nil {
			existing.SessionID = row.SessionID
			existing.Title = row.Title
			existing.MessagesJSON = row.MessagesJSON
			existing.Progress = row.Progress
			if err := globalDB.Save(&existing).Error; err != nil {
				http.Error(w, "failed to save chat", 500)
				return
			}
		} else if err := globalDB.Create(&row).Error; err != nil {
			http.Error(w, "failed to create chat", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
