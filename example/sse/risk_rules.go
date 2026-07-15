package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type RiskRule struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Code        string    `gorm:"size:32;uniqueIndex" json:"code"`
	Name        string    `gorm:"size:128" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Level       string    `gorm:"size:16;index" json:"level"`
	Keywords    string    `gorm:"type:text" json:"keywords"`
	Enabled     bool      `gorm:"default:true;index" json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func seedRiskRules() error {
	defaults := []RiskRule{{Code: "P1-EMERGENCY", Name: "立即急诊危险信号", Description: "意识障碍、严重呼吸困难、持续胸痛、大出血等", Level: "P1", Keywords: "意识障碍,呼吸困难,持续胸痛,大出血", Enabled: true}, {Code: "P2-URGENT", Name: "尽快就诊风险信号", Description: "高热不退、症状进行性加重、特殊人群等", Level: "P2", Keywords: "高热不退,进行性加重,孕产妇,儿童", Enabled: true}, {Code: "P3-ROUTINE", Name: "普通门诊建议", Description: "未发现急危重信号且症状相对稳定", Level: "P3", Keywords: "症状稳定", Enabled: true}}
	for _, rule := range defaults {
		var count int64
		if err := globalDB.Model(&RiskRule{}).Where("code = ?", rule.Code).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := globalDB.Create(&rule).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func adminRiskRulesHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var rows []RiskRule
		if globalDB.Order("level, id").Find(&rows).Error != nil {
			http.Error(w, "query failed", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(rows)
	case http.MethodPost:
		var req RiskRule
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		req.Code = strings.TrimSpace(req.Code)
		req.Name = strings.TrimSpace(req.Name)
		req.Level = strings.ToUpper(strings.TrimSpace(req.Level))
		if req.Code == "" || req.Name == "" || (req.Level != "P1" && req.Level != "P2" && req.Level != "P3") {
			http.Error(w, "invalid rule", 400)
			return
		}
		req.Enabled = true
		if globalDB.Create(&req).Error != nil {
			http.Error(w, "create failed", 500)
			return
		}
		writeAudit(session.DisplayName, "risk_rule_create", req.Code, req.Level)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(req)
	default:
		http.Error(w, "method not allowed", 405)
	}
}
func adminRiskRuleUpdateHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := requireAdminAPI(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		ID      uint  `json:"id"`
		Enabled *bool `json:"enabled"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == 0 || req.Enabled == nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if globalDB.Model(&RiskRule{}).Where("id = ?", req.ID).Update("enabled", *req.Enabled).Error != nil {
		http.Error(w, "update failed", 500)
		return
	}
	writeAudit(session.DisplayName, "risk_rule_update", string(rune(req.ID)), "")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "enabled": *req.Enabled})
}
