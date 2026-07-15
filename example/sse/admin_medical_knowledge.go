package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

func adminMedicalKnowledgeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		var rows []MedicalKnowledgeDocument
		query := globalDB.Order("updated_at DESC, id DESC")
		if status := strings.TrimSpace(r.URL.Query().Get("reviewStatus")); status != "" {
			query = query.Where("review_status = ?", status)
		}
		if err := query.Find(&rows).Error; err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(rows)
	case http.MethodPost:
		var req struct {
			ID         uint   `json:"id"`
			Code       string `json:"code"`
			Title      string `json:"title"`
			Content    string `json:"content"`
			Keywords   string `json:"keywords"`
			SourceName string `json:"sourceName"`
			SourceURL  string `json:"sourceUrl"`
			Action     string `json:"action"`
			Enabled    *bool  `json:"enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(req.Action)
		if req.ID == 0 {
			if action != "create" {
				http.Error(w, "id is required", http.StatusBadRequest)
				return
			}
			row := MedicalKnowledgeDocument{
				Code: strings.TrimSpace(req.Code), Title: strings.TrimSpace(req.Title), Content: strings.TrimSpace(req.Content),
				Keywords: strings.TrimSpace(req.Keywords), SourceName: strings.TrimSpace(req.SourceName), SourceURL: strings.TrimSpace(req.SourceURL),
				Enabled: false, ReviewStatus: "pending", Version: 1,
			}
			if row.Code == "" || row.Title == "" || row.Content == "" || row.SourceName == "" || !validKnowledgeSourceURL(row.SourceURL) {
				http.Error(w, "invalid knowledge document", http.StatusBadRequest)
				return
			}
			if err := globalDB.Create(&row).Error; err != nil {
				http.Error(w, "create failed", http.StatusConflict)
				return
			}
			writeAudit(session.DisplayName, "medical_knowledge_create", row.Code, row.SourceURL)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		var row MedicalKnowledgeDocument
		if err := globalDB.First(&row, req.ID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "knowledge document not found", http.StatusNotFound)
			} else {
				http.Error(w, "query failed", http.StatusInternalServerError)
			}
			return
		}
		now := time.Now()
		updates := map[string]any{}
		switch action {
		case "approve":
			updates["review_status"] = "approved"
			updates["enabled"] = true
			updates["reviewed_by"] = session.DisplayName
			updates["reviewed_at"] = now
		case "reject":
			updates["review_status"] = "rejected"
			updates["enabled"] = false
			updates["reviewed_by"] = session.DisplayName
			updates["reviewed_at"] = now
		case "toggle":
			if req.Enabled == nil || row.ReviewStatus != "approved" {
				http.Error(w, "only approved knowledge can be enabled", http.StatusConflict)
				return
			}
			updates["enabled"] = *req.Enabled
		case "update":
			if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.SourceName) == "" || !validKnowledgeSourceURL(req.SourceURL) {
				http.Error(w, "invalid knowledge document", http.StatusBadRequest)
				return
			}
			updates["title"] = strings.TrimSpace(req.Title)
			updates["content"] = strings.TrimSpace(req.Content)
			updates["keywords"] = strings.TrimSpace(req.Keywords)
			updates["source_name"] = strings.TrimSpace(req.SourceName)
			updates["source_url"] = strings.TrimSpace(req.SourceURL)
			updates["review_status"] = "pending"
			updates["enabled"] = false
			updates["version"] = row.Version + 1
			updates["reviewed_by"] = ""
			updates["reviewed_at"] = nil
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
		if err := globalDB.Model(&row).Updates(updates).Error; err != nil {
			http.Error(w, "update failed", http.StatusInternalServerError)
			return
		}
		writeAudit(session.DisplayName, "medical_knowledge_"+action, row.Code, "")
		if err := globalDB.First(&row, row.ID).Error; err != nil {
			http.Error(w, "reload failed", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(row)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func validKnowledgeSourceURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "https://") && len(value) <= 512
}
