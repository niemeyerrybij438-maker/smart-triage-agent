package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	followUpRecipientPatient = "patient"
	followUpRecipientDoctor  = "doctor"
	followUpAllDoctors       = "all-doctors"
	followUpNoticeDueSoon    = "due_soon"
	followUpNoticeOverdue    = "overdue"
	followUpNoticeWorsened   = "worsened"
	followUpNoticeResolved   = "resolved"
)

type FollowUpNotification struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	FollowUpPlanID uint       `gorm:"not null;uniqueIndex:idx_follow_up_notice_dedupe,priority:1;index" json:"followUpPlanId"`
	SessionID      string     `gorm:"size:64;index" json:"sessionId"`
	RecipientType  string     `gorm:"size:24;not null;uniqueIndex:idx_follow_up_notice_dedupe,priority:2;index" json:"recipientType"`
	RecipientKey   string     `gorm:"size:128;not null;uniqueIndex:idx_follow_up_notice_dedupe,priority:3;index" json:"recipientKey,omitempty"`
	Kind           string     `gorm:"size:32;not null;uniqueIndex:idx_follow_up_notice_dedupe,priority:4;index" json:"kind"`
	Title          string     `gorm:"size:160;not null" json:"title"`
	Content        string     `gorm:"type:text" json:"content"`
	Read           bool       `gorm:"column:is_read;default:false;index" json:"read"`
	ReadAt         *time.Time `json:"readAt"`
	CreatedAt      time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func startFollowUpNotificationLoop() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := maybeGenerateFollowUpNotifications(time.Now()); err != nil {
				log.Printf("follow-up notification refresh failed: %v", err)
			}
		}
	}()
}

var followUpNotificationGenerationMu sync.Mutex
var followUpNotificationGeneratedAt time.Time
var followUpNotificationGeneratedDB *gorm.DB

const followUpNotificationReadRefreshInterval = 5 * time.Second

func generateFollowUpNotifications(now time.Time) error {
	followUpNotificationGenerationMu.Lock()
	defer followUpNotificationGenerationMu.Unlock()
	err := generateFollowUpNotificationsLocked(now)
	if err == nil {
		followUpNotificationGeneratedAt = now
		followUpNotificationGeneratedDB = globalDB
	}
	return err
}

func maybeGenerateFollowUpNotifications(now time.Time) error {
	followUpNotificationGenerationMu.Lock()
	defer followUpNotificationGenerationMu.Unlock()
	if followUpNotificationGeneratedDB == globalDB && !followUpNotificationGeneratedAt.IsZero() && now.Sub(followUpNotificationGeneratedAt) < followUpNotificationReadRefreshInterval {
		return nil
	}
	err := generateFollowUpNotificationsLocked(now)
	if err == nil {
		followUpNotificationGeneratedAt = now
		followUpNotificationGeneratedDB = globalDB
	}
	return err
}

func generateFollowUpNotificationsLocked(now time.Time) error {
	if globalDB == nil {
		return nil
	}
	return globalDB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&FollowUpPlan{}).
			Where("status = ? AND scheduled_at < ?", "pending", now).
			Updates(map[string]any{"status": "overdue", "updated_at": now}).Error; err != nil {
			return err
		}

		var plans []FollowUpPlan
		if err := tx.Where("status IN ? OR (status = ? AND patient_outcome = ? AND resolved_at IS NOT NULL)", []string{"pending", "overdue", "escalated"}, "completed", "worsened").Find(&plans).Error; err != nil {
			return err
		}
		for _, plan := range plans {
			for _, target := range followUpNotificationTargets(plan, now) {
				if err := createFollowUpNotification(tx, plan, target.RecipientType, target.RecipientKey, target.Kind); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type followUpNotificationTarget struct {
	RecipientType string
	RecipientKey  string
	Kind          string
}

func followUpNotificationTargets(plan FollowUpPlan, now time.Time) []followUpNotificationTarget {
	switch plan.Status {
	case "pending":
		if !plan.ScheduledAt.After(now.Add(24 * time.Hour)) {
			return []followUpNotificationTarget{{RecipientType: followUpRecipientPatient, RecipientKey: plan.PatientPhone, Kind: followUpNoticeDueSoon}}
		}
	case "overdue":
		return []followUpNotificationTarget{
			{RecipientType: followUpRecipientPatient, RecipientKey: plan.PatientPhone, Kind: followUpNoticeOverdue},
			{RecipientType: followUpRecipientDoctor, RecipientKey: followUpAllDoctors, Kind: followUpNoticeOverdue},
		}
	case "escalated":
		return []followUpNotificationTarget{{RecipientType: followUpRecipientDoctor, RecipientKey: followUpAllDoctors, Kind: followUpNoticeWorsened}}
	case "completed":
		if plan.PatientOutcome == "worsened" && plan.ResolvedAt != nil {
			return []followUpNotificationTarget{{RecipientType: followUpRecipientPatient, RecipientKey: plan.PatientPhone, Kind: followUpNoticeResolved}}
		}
	}
	return nil
}

func createFollowUpNotification(tx *gorm.DB, plan FollowUpPlan, recipientType, recipientKey, kind string) error {
	if strings.TrimSpace(recipientKey) == "" {
		return nil
	}
	title, content := followUpNotificationCopy(plan, kind)
	notification := FollowUpNotification{
		FollowUpPlanID: plan.ID,
		SessionID:      plan.SessionID,
		RecipientType:  recipientType,
		RecipientKey:   recipientKey,
		Kind:           kind,
		Title:          title,
		Content:        content,
	}
	return tx.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"title", "content", "updated_at"})}).Create(&notification).Error
}

func followUpNotificationCopy(plan FollowUpPlan, kind string) (string, string) {
	when := plan.ScheduledAt.Format("01月02日 15:04")
	department := strings.TrimSpace(plan.Department)
	if department == "" {
		department = "相关科室"
	}
	switch kind {
	case followUpNoticeDueSoon:
		return "复诊随访即将到期", fmt.Sprintf("您在%s有一项%s随访，请及时查看医生建议并提交反馈。", when, department)
	case followUpNoticeOverdue:
		return "复诊随访已逾期", fmt.Sprintf("原定%s的%s随访尚未完成，请尽快处理。", when, department)
	case followUpNoticeWorsened:
		return "患者随访反馈症状加重", fmt.Sprintf("患者在%s随访中反馈症状加重，请及时复核并联系患者。", department)
	case followUpNoticeResolved:
		return "医生已处理您的异常反馈", fmt.Sprintf("处置方式：%s；医生意见：%s。", followUpResolutionLabel(plan.ResolutionType), followUpResolutionText(plan.ResolutionType, plan.DoctorResolution))
	default:
		return "复诊随访提醒", "您有一项复诊随访需要处理。"
	}
}

func followUpNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	if err := maybeGenerateFollowUpNotifications(time.Now()); err != nil {
		http.Error(w, "failed to refresh notifications", http.StatusInternalServerError)
		return
	}
	recipientType, recipientKey, ok := followUpNotificationRecipient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		var items []FollowUpNotification
		query := globalDB.Where("recipient_type = ? AND recipient_key = ?", recipientType, recipientKey)
		if err := query.Order("is_read ASC, created_at DESC").Limit(100).Find(&items).Error; err != nil {
			log.Printf("follow-up notification query failed: %v", err)
			http.Error(w, "failed to query notifications", http.StatusInternalServerError)
			return
		}
		var unreadCount int64
		_ = query.Where("is_read = ?", false).Count(&unreadCount).Error
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "unreadCount": unreadCount})
	case http.MethodPost:
		var req struct {
			ID  uint `json:"id"`
			All bool `json:"all"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || (!req.All && req.ID == 0) {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		now := time.Now()
		query := globalDB.Model(&FollowUpNotification{}).
			Where("recipient_type = ? AND recipient_key = ? AND is_read = ?", recipientType, recipientKey, false)
		if !req.All {
			query = query.Where("id = ?", req.ID)
		}
		result := query.Updates(map[string]any{"is_read": true, "read_at": now, "updated_at": now})
		if result.Error != nil {
			http.Error(w, "failed to update notification", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": result.RowsAffected})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func followUpNotificationRecipient(r *http.Request) (string, string, bool) {
	if patient, ok := currentPatient(r); ok {
		return followUpRecipientPatient, patient.Phone, true
	}
	if _, ok := currentDoctorSession(r); ok {
		return followUpRecipientDoctor, followUpAllDoctors, true
	}
	return "", "", false
}

func resetPendingFollowUpNotifications(planID uint) {
	if globalDB == nil || planID == 0 {
		return
	}
	_ = globalDB.Where("follow_up_plan_id = ? AND kind IN ?", planID, []string{followUpNoticeDueSoon, followUpNoticeOverdue}).
		Delete(&FollowUpNotification{}).Error
}

func completePatientFollowUpNotifications(planID uint, now time.Time) {
	if globalDB == nil || planID == 0 {
		return
	}
	_ = globalDB.Model(&FollowUpNotification{}).
		Where("follow_up_plan_id = ? AND recipient_type = ? AND is_read = ?", planID, followUpRecipientPatient, false).
		Updates(map[string]any{"is_read": true, "read_at": now, "updated_at": now}).Error
}

func migrateLegacyFollowUpNotificationReadState() error {
	if globalDB == nil || !globalDB.Migrator().HasTable(&FollowUpNotification{}) {
		return nil
	}
	columns, err := globalDB.Migrator().ColumnTypes(&FollowUpNotification{})
	if err != nil {
		return err
	}
	hasLegacyRead := false
	for _, column := range columns {
		if strings.EqualFold(column.Name(), "read") {
			hasLegacyRead = true
			break
		}
	}
	if !hasLegacyRead {
		return nil
	}
	tableName := globalDB.NamingStrategy.TableName("FollowUpNotification")
	statement := fmt.Sprintf("UPDATE `%s` SET is_read = `read` WHERE `read` = ?", tableName)
	return globalDB.Exec(statement, true).Error
}

func adminFollowUpDetailsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	if globalDB == nil {
		http.Error(w, "database is not ready", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := maybeGenerateFollowUpNotifications(time.Now()); err != nil {
		log.Printf("admin follow-up refresh failed: %v", err)
		http.Error(w, "failed to refresh follow-ups", http.StatusInternalServerError)
		return
	}
	filters, err := parseAdminFollowUpFilters(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var plans []FollowUpPlan
	planQuery := applyAdminFollowUpPlanFilters(globalDB.Model(&FollowUpPlan{}), filters)
	if err := planQuery.Order("created_at DESC").Limit(1000).Find(&plans).Error; err != nil {
		http.Error(w, "failed to query follow-ups", http.StatusInternalServerError)
		return
	}
	var notifications []FollowUpNotification
	notificationQuery := applyAdminFollowUpNotificationFilters(globalDB.Model(&FollowUpNotification{}), filters)
	if err := notificationQuery.Order("created_at DESC").Limit(2000).Find(&notifications).Error; err != nil {
		http.Error(w, "failed to query follow-up notifications", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plans":         plans,
		"notifications": notifications,
	})
}
