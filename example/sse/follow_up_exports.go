package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type adminFollowUpFilters struct {
	Keyword       string
	Status        string
	Department    string
	Doctor        string
	Kind          string
	RecipientType string
	Read          string
	From          *time.Time
	To            *time.Time
}

func parseAdminFollowUpFilters(r *http.Request) (adminFollowUpFilters, error) {
	filters := adminFollowUpFilters{
		Keyword:       strings.TrimSpace(r.URL.Query().Get("keyword")),
		Status:        strings.TrimSpace(r.URL.Query().Get("status")),
		Department:    strings.TrimSpace(r.URL.Query().Get("department")),
		Doctor:        strings.TrimSpace(r.URL.Query().Get("doctor")),
		Kind:          strings.TrimSpace(r.URL.Query().Get("kind")),
		RecipientType: strings.TrimSpace(r.URL.Query().Get("recipientType")),
		Read:          strings.TrimSpace(r.URL.Query().Get("read")),
	}
	if value := strings.TrimSpace(r.URL.Query().Get("from")); value != "" {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			return filters, fmt.Errorf("invalid from date")
		}
		filters.From = &parsed
	}
	if value := strings.TrimSpace(r.URL.Query().Get("to")); value != "" {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			return filters, fmt.Errorf("invalid to date")
		}
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		filters.To = &parsed
	}
	if filters.From != nil && filters.To != nil && filters.From.After(*filters.To) {
		return filters, fmt.Errorf("from date must not be after to date")
	}
	return filters, nil
}

func applyAdminFollowUpPlanFilters(query *gorm.DB, filters adminFollowUpFilters) *gorm.DB {
	if filters.Keyword != "" {
		like := "%" + filters.Keyword + "%"
		query = query.Where("patient_phone LIKE ? OR session_id LIKE ? OR symptom LIKE ? OR department LIKE ? OR created_by LIKE ?", like, like, like, like, like)
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.Department != "" {
		query = query.Where("department = ?", filters.Department)
	}
	if filters.Doctor != "" {
		query = query.Where("created_by = ?", filters.Doctor)
	}
	if filters.From != nil {
		query = query.Where("scheduled_at >= ?", *filters.From)
	}
	if filters.To != nil {
		query = query.Where("scheduled_at <= ?", *filters.To)
	}
	return query
}

func applyAdminFollowUpNotificationFilters(query *gorm.DB, filters adminFollowUpFilters) *gorm.DB {
	if filters.Keyword != "" {
		like := "%" + filters.Keyword + "%"
		query = query.Where("recipient_key LIKE ? OR session_id LIKE ? OR title LIKE ? OR content LIKE ?", like, like, like, like)
	}
	if filters.Kind != "" {
		query = query.Where("kind = ?", filters.Kind)
	}
	if filters.RecipientType != "" {
		query = query.Where("recipient_type = ?", filters.RecipientType)
	}
	switch filters.Read {
	case "read":
		query = query.Where("is_read = ?", true)
	case "unread":
		query = query.Where("is_read = ?", false)
	}
	if filters.From != nil {
		query = query.Where("created_at >= ?", *filters.From)
	}
	if filters.To != nil {
		query = query.Where("created_at <= ?", *filters.To)
	}
	return query
}

func adminFollowUpExportHandler(w http.ResponseWriter, r *http.Request) {
	admin, ok := requireAdminAPI(w, r)
	if !ok {
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
	filters, err := parseAdminFollowUpFilters(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	exportType := strings.TrimSpace(r.URL.Query().Get("type"))
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	var rowCount int
	switch exportType {
	case "plans":
		rowCount, err = writeFollowUpPlansCSV(writer, filters)
	case "notifications":
		rowCount, err = writeFollowUpNotificationsCSV(writer, filters)
	default:
		http.Error(w, "invalid export type", http.StatusBadRequest)
		return
	}
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	if err != nil {
		http.Error(w, "failed to export follow-up data", http.StatusInternalServerError)
		return
	}
	filename := fmt.Sprintf("follow-up-%s-%s.csv", exportType, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	writeAudit(admin.DisplayName, "follow_up_export", exportType, fmt.Sprintf("rows=%d; filters=%s", rowCount, adminFollowUpFilterSummary(filters)))
	_, _ = w.Write(buffer.Bytes())
}

func writeFollowUpPlansCSV(writer *csv.Writer, filters adminFollowUpFilters) (int, error) {
	var plans []FollowUpPlan
	query := applyAdminFollowUpPlanFilters(globalDB.Model(&FollowUpPlan{}), filters)
	if err := query.Order("scheduled_at DESC").Limit(5000).Find(&plans).Error; err != nil {
		return 0, err
	}
	if err := writer.Write([]string{"随访编号", "会话编号", "患者手机号", "症状", "科室", "随访时间", "状态", "创建医生", "医生建议", "注意事项", "患者反馈", "反馈说明", "反馈时间", "处置方式", "医生处置意见", "处理医生", "处理时间", "创建时间"}); err != nil {
		return 0, err
	}
	for _, plan := range plans {
		row := []string{
			strconv.FormatUint(uint64(plan.ID), 10),
			csvSafe(plan.SessionID),
			maskFollowUpPhone(plan.PatientPhone),
			csvSafe(plan.Symptom),
			csvSafe(plan.Department),
			formatCSVTime(plan.ScheduledAt),
			followUpStatusCSVLabel(plan.Status),
			csvSafe(plan.CreatedBy),
			csvSafe(plan.Advice),
			csvSafe(plan.Precautions),
			followUpOutcomeCSVLabel(plan.PatientOutcome),
			csvSafe(plan.PatientNote),
			formatCSVTimePointer(plan.RespondedAt),
			followUpResolutionCSVLabel(plan),
			csvSafe(plan.DoctorResolution),
			csvSafe(plan.ResolvedBy),
			formatCSVTimePointer(plan.ResolvedAt),
			formatCSVTime(plan.CreatedAt),
		}
		if err := writer.Write(row); err != nil {
			return 0, err
		}
	}
	return len(plans), nil
}

func writeFollowUpNotificationsCSV(writer *csv.Writer, filters adminFollowUpFilters) (int, error) {
	var notifications []FollowUpNotification
	query := applyAdminFollowUpNotificationFilters(globalDB.Model(&FollowUpNotification{}), filters)
	if err := query.Order("created_at DESC").Limit(10000).Find(&notifications).Error; err != nil {
		return 0, err
	}
	if err := writer.Write([]string{"提醒编号", "随访编号", "会话编号", "接收对象", "提醒类型", "标题", "内容", "阅读状态", "阅读时间", "生成时间"}); err != nil {
		return 0, err
	}
	for _, notification := range notifications {
		recipient := "全部医生"
		if notification.RecipientType == followUpRecipientPatient {
			recipient = "患者 " + maskFollowUpPhone(notification.RecipientKey)
		}
		readStatus := "未读"
		if notification.Read {
			readStatus = "已读"
		}
		row := []string{
			strconv.FormatUint(uint64(notification.ID), 10),
			strconv.FormatUint(uint64(notification.FollowUpPlanID), 10),
			csvSafe(notification.SessionID),
			csvSafe(recipient),
			followUpNoticeCSVLabel(notification.Kind),
			csvSafe(notification.Title),
			csvSafe(notification.Content),
			readStatus,
			formatCSVTimePointer(notification.ReadAt),
			formatCSVTime(notification.CreatedAt),
		}
		if err := writer.Write(row); err != nil {
			return 0, err
		}
	}
	return len(notifications), nil
}

func maskFollowUpPhone(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 7 {
		return value
	}
	return value[:3] + "****" + value[len(value)-4:]
}

func csvSafe(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func formatCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func formatCSVTimePointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatCSVTime(*value)
}

func followUpStatusCSVLabel(value string) string {
	return map[string]string{"pending": "待反馈", "completed": "已完成", "overdue": "已逾期", "escalated": "异常升级"}[value]
}

func followUpOutcomeCSVLabel(value string) string {
	return map[string]string{"improved": "症状好转", "unchanged": "无明显变化", "worsened": "症状加重"}[value]
}

func followUpResolutionCSVLabel(plan FollowUpPlan) string {
	if plan.ResolvedAt == nil {
		return ""
	}
	return followUpResolutionLabel(plan.ResolutionType)
}

func followUpNoticeCSVLabel(value string) string {
	return map[string]string{"due_soon": "即将到期", "overdue": "已逾期", "worsened": "症状加重", "resolved": "医生已处理"}[value]
}

func adminFollowUpFilterSummary(filters adminFollowUpFilters) string {
	parts := []string{
		"keyword=" + filters.Keyword,
		"status=" + filters.Status,
		"department=" + filters.Department,
		"doctor=" + filters.Doctor,
		"kind=" + filters.Kind,
		"recipientType=" + filters.RecipientType,
		"read=" + filters.Read,
	}
	if filters.From != nil {
		parts = append(parts, "from="+filters.From.Format("2006-01-02"))
	}
	if filters.To != nil {
		parts = append(parts, "to="+filters.To.Format("2006-01-02"))
	}
	return strings.Join(parts, ";")
}
