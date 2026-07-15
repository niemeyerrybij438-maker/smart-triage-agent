package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

type specialistResult struct {
	Agent       string   `json:"agent"`
	Summary     string   `json:"summary"`
	Intent      string   `json:"intent,omitempty"`
	RiskLevel   string   `json:"riskLevel,omitempty"`
	Department  string   `json:"department,omitempty"`
	Warning     string   `json:"warning,omitempty"`
	Question    string   `json:"question,omitempty"`
	Action      string   `json:"action,omitempty"`
	Confidence  int      `json:"confidence,omitempty"`
	MissingInfo []string `json:"missingInfo,omitempty"`
}

type dispatchPlan struct {
	Agents []string `json:"agents"`
	Reason string   `json:"reason"`
}

type AgentTrace struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	SessionID    string    `gorm:"size:64;index" json:"sessionId"`
	PatientPhone string    `gorm:"size:32;index" json:"patientPhone"`
	AgentName    string    `gorm:"size:64;index" json:"agentName"`
	Status       string    `gorm:"size:32;index" json:"status"`
	DurationMS   int64     `json:"durationMs"`
	Summary      string    `gorm:"type:text" json:"summary"`
	ErrorText    string    `gorm:"type:text" json:"errorText"`
	CreatedAt    time.Time `gorm:"index" json:"createdAt"`
}

type specialistAgent struct {
	name   string
	runner *adk.TypedRunner[*schema.AgenticMessage]
}

var globalIntentRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalRiskRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalDepartmentRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalHistoryRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalMedicationRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalEmergencyRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalSupervisorRunner *adk.TypedRunner[*schema.AgenticMessage]
var specialistSlots = make(chan struct{}, 5)

func specialistInstruction(role string) string {
	common := "\u4f60\u662f\u4f01\u4e1a\u7ea7\u533b\u7597\u5bfc\u8bca\u7cfb\u7edf\u4e2d\u7684\u72ec\u7acb\u4e13\u4e1a Agent\u3002\u53ea\u8f93\u51fa JSON\uff0c\u4e0d\u8981 Markdown\uff0c\u4e0d\u786e\u8bca\u3001\u4e0d\u7f16\u9020\u3002\u8f93\u5165\u5305\u542b\u5f53\u524d\u7528\u6237\u6d88\u606f\u548c\u4e3b\u5bfc\u8bca Agent \u57fa\u4e8e\u5b8c\u6574\u4f1a\u8bdd\u8bb0\u5fc6\u751f\u6210\u7684\u8349\u7a3f\u3002"
	switch role {
	case "intent":
		return common + ` \u8f93\u51fa\uff1a{"agent":"intent","intent":"symptom_triage|medication|report|emergency|followup|other","summary":"\u4e0a\u4e0b\u6587\u76f8\u5173\u7684\u610f\u56fe\u6458\u8981","confidence":0}`
	case "risk":
		return common + ` \u72ec\u7acb\u8bc6\u522b\u5371\u9669\u4fe1\u53f7\u3002\u8f93\u51fa\uff1a{"agent":"risk","riskLevel":"P1|P2|P3","summary":"\u98ce\u9669\u4f9d\u636e","warning":"\u5fc5\u8981\u7684\u5b89\u5168\u63d0\u9192","confidence":0}\u3002\u53ea\u6709\u660e\u786e\u6025\u5371\u91cd\u4fe1\u53f7\u624d\u7528 P1\u3002`
	case "department":
		return common + ` \u72ec\u7acb\u63a8\u8350\u5c31\u8bca\u79d1\u5ba4\u3002\u8f93\u51fa\uff1a{"agent":"department","department":"\u79d1\u5ba4\u540d\u79f0","summary":"\u63a8\u8350\u4f9d\u636e","confidence":0}\u3002\u4fe1\u606f\u4e0d\u8db3\u65f6\u4f7f\u7528\u5168\u79d1\u533b\u5b66\u79d1\u3002`
	case "history":
		return common + ` \u8bc6\u522b\u5df2\u77e5\u75c5\u53f2\u548c\u5f53\u524d\u6700\u7f3a\u5931\u7684\u5173\u952e\u4fe1\u606f\u3002\u8f93\u51fa\uff1a{"agent":"history","summary":"\u5df2\u77e5\u75c5\u53f2\u6458\u8981","missingInfo":["\u7f3a\u5931\u4fe1\u606f"],"question":"\u6700\u591a\u4e00\u4e2a\u5fc5\u8981\u8ffd\u95ee","confidence":0}\u3002\u4e0d\u8981\u91cd\u590d\u8be2\u95ee\u7528\u6237\u5df2\u56de\u7b54\u7684\u4fe1\u606f\u3002`
	case "medication":
		return common + ` \u4e13\u95e8\u5ba1\u67e5\u7528\u836f\u5b89\u5168\u3001\u8fc7\u654f\u3001\u5b55\u671f\u3001\u513f\u7ae5\u3001\u8001\u5e74\u4eba\u548c\u5408\u5e76\u7528\u836f\u98ce\u9669\u3002\u8f93\u51fa\uff1a{"agent":"medication","summary":"\u7528\u836f\u8fb9\u754c\u4e0e\u98ce\u9669","warning":"\u5b89\u5168\u63d0\u9192","question":"\u5fc5\u8981\u65f6\u7684\u4e00\u4e2a\u8ffd\u95ee","confidence":0}\u3002\u4e0d\u5f00\u5904\u65b9\u3001\u4e0d\u81ea\u884c\u7ed9\u9ad8\u98ce\u9669\u4eba\u7fa4\u5177\u4f53\u5242\u91cf\u3002`
	case "emergency":
		return common + ` \u4e13\u95e8\u5224\u65ad\u662f\u5426\u9700\u8981\u7acb\u5373\u6025\u8bca\u3001\u62e8\u6253 120 \u6216\u5f00\u542f\u6025\u8bca\u7eff\u8272\u901a\u9053\u3002\u8f93\u51fa\uff1a{"agent":"emergency","action":"routine|urgent_outpatient|emergency|call_120","summary":"\u5904\u7f6e\u4f9d\u636e","warning":"\u7acb\u5373\u884c\u52a8\u5efa\u8bae","confidence":0}\u3002`
	default:
		return common
	}
}

func supervisorInstruction() string {
	return strings.Join([]string{
		"\u4f60\u662f\u4f01\u4e1a\u7ea7\u591a Agent \u5bfc\u8bca\u7cfb\u7edf\u7684 Supervisor Agent\u3002",
		"\u8f93\u5165\u5305\u542b\u4e3b\u5bfc\u8bca\u8349\u7a3f\u3001\u52a8\u6001\u8c03\u5ea6\u8ba1\u5212\u4e0e\u4e13\u4e1a Agent JSON \u7ed3\u679c\u3002",
		"\u7efc\u5408\u5404\u65b9\u7ed3\u8bba\u751f\u6210\u6700\u7ec8\u60a3\u8005\u56de\u7b54\uff1b\u5fc5\u987b\u627f\u63a5\u4e0a\u4e0b\u6587\u5e76\u76f4\u63a5\u56de\u7b54\u5f53\u524d\u95ee\u9898\u3002",
		"\u6025\u8bca\u4e0e\u98ce\u9669\u7ed3\u8bba\u4f18\u5148\u7ea7\u6700\u9ad8\uff1b\u7528\u836f\u95ee\u9898\u5fc5\u987b\u91c7\u7eb3\u7528\u836f\u5b89\u5168 Agent \u7684\u8fb9\u754c\u3002",
		"\u82e5\u591a\u4e2a Agent \u7ed3\u8bba\u51b2\u7a81\uff0c\u9009\u62e9\u66f4\u4fdd\u5b88\u7684\u98ce\u9669\u5904\u7f6e\uff0c\u5e76\u5efa\u8bae\u4eba\u5de5\u533b\u751f\u590d\u6838\u3002",
		"\u4e0d\u8981\u63d0\u53ca Agent\u3001JSON\u3001\u8c03\u5ea6\u6216\u6a21\u578b\u3002\u9ed8\u8ba4 100-200 \u5b57\uff0c\u6700\u591a\u8ffd\u95ee\u4e00\u4e2a\u5173\u952e\u95ee\u9898\u3002",
		"\u53ea\u8f93\u51fa\u6700\u7ec8\u60a3\u8005\u56de\u7b54\u6b63\u6587\u3002",
	}, "\n")
}

func parseSpecialistResult(name, text string) specialistResult {
	result := specialistResult{Agent: name, Summary: strings.TrimSpace(text)}
	_ = json.Unmarshal([]byte(normalizeExtractorJSON(text)), &result)
	if strings.TrimSpace(result.Agent) == "" {
		result.Agent = name
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 100 {
		result.Confidence = 100
	}
	return result
}

func saveAgentTrace(sessionID, phone, name, status string, started time.Time, summary string, err error) {
	if globalDB == nil {
		return
	}
	row := AgentTrace{SessionID: sessionID, PatientPhone: phone, AgentName: name, Status: status, DurationMS: time.Since(started).Milliseconds(), Summary: strings.TrimSpace(summary)}
	if err != nil {
		row.ErrorText = err.Error()
	}
	_ = globalDB.Create(&row).Error
}

func runSpecialist(ctx context.Context, agent specialistAgent, sessionID, phone, prompt string) specialistResult {
	started := time.Now()
	if agent.runner == nil {
		err := fmt.Errorf("agent runner unavailable")
		saveAgentTrace(sessionID, phone, agent.name, "degraded", started, "", err)
		return specialistResult{Agent: agent.name, Summary: "\u4e13\u4e1a\u5206\u6790\u6682\u65f6\u4e0d\u53ef\u7528"}
	}
	specialistSlots <- struct{}{}
	defer func() { <-specialistSlots }()
	runCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	text, err := collectAgentReply(runCtx, agent.runner, []*schema.AgenticMessage{schema.UserAgenticMessage(prompt)})
	status := "completed"
	if err != nil {
		status = "degraded"
		log.Printf("multi-agent specialist failed: session=%s agent=%s err=%v", sessionID, agent.name, err)
	}
	saveAgentTrace(sessionID, phone, agent.name, status, started, text, err)
	if err != nil {
		return specialistResult{Agent: agent.name, Summary: "\u4e13\u4e1a\u5206\u6790\u6682\u65f6\u4e0d\u53ef\u7528"}
	}
	return parseSpecialistResult(agent.name, text)
}

func containsAny(value string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func buildDispatchPlan(message string, intent specialistResult) dispatchPlan {
	selected := []string{"risk", "department", "history"}
	if intent.Intent == "medication" || hasPositiveDispatchKeyword(message, "药", "剂量", "怎么吃", "怎么涂", "过敏", "怀孕", "孕妇", "儿童") {
		selected = append(selected, "medication")
	}
	if intent.Intent == "emergency" || hasPositiveDispatchKeyword(message, "胸痛", "呼吸困难", "喘不上气", "昏迷", "意识", "大出血", "抽搐", "120", "急诊") {
		selected = append(selected, "emergency")
	}
	return dispatchPlan{Agents: selected, Reason: "intent=" + intent.Intent}
}

func hasPositiveDispatchKeyword(message string, keywords ...string) bool {
	normalized := normalizeKnowledgeText(message)
	for _, keyword := range keywords {
		normalizedKeyword := normalizeKnowledgeText(keyword)
		if strings.Contains(normalized, normalizedKeyword) && !keywordNegated(normalized, normalizedKeyword) {
			return true
		}
	}
	return false
}

func runnerForAgent(name string) *adk.TypedRunner[*schema.AgenticMessage] {
	switch name {
	case "risk":
		return globalRiskRunner
	case "department":
		return globalDepartmentRunner
	case "history":
		return globalHistoryRunner
	case "medication":
		return globalMedicationRunner
	case "emergency":
		return globalEmergencyRunner
	default:
		return nil
	}
}

func evaluateEscalation(results []specialistResult) (bool, string) {
	lowConfidence := false
	emergency := false
	riskP1 := false
	for _, result := range results {
		if result.Confidence > 0 && result.Confidence < 55 {
			lowConfidence = true
		}
		if result.RiskLevel == "P1" {
			riskP1 = true
		}
		if result.Action == "emergency" || result.Action == "call_120" {
			emergency = true
		}
	}
	if riskP1 || emergency {
		return true, "high_risk"
	}
	if lowConfidence {
		return true, "low_confidence"
	}
	return false, ""
}

func buildSpecialistPrompt(userMessage, healthContext, knowledgeContext, draft string) string {
	promptParts := []string{"<user_message>", userMessage, "</user_message>"}
	if strings.TrimSpace(healthContext) != "" {
		promptParts = append(promptParts, healthContext)
	}
	if strings.TrimSpace(knowledgeContext) != "" {
		promptParts = append(promptParts, knowledgeContext)
	}
	if strings.TrimSpace(draft) != "" {
		promptParts = append(promptParts, "<contextual_draft>", draft, "</contextual_draft>")
	}
	return strings.Join(promptParts, "\n")
}

func orchestrateTriage(ctx context.Context, sessionID, phone, userMessage, healthContext, knowledgeContext, draft string, intent specialistResult) (string, []specialistResult) {
	prompt := buildSpecialistPrompt(userMessage, healthContext, knowledgeContext, draft)
	plan := buildDispatchPlan(userMessage, intent)
	planJSON, _ := json.Marshal(plan)
	saveAgentTrace(sessionID, phone, "dispatcher", "completed", time.Now(), string(planJSON), nil)

	results := make([]specialistResult, len(plan.Agents)+1)
	results[0] = intent
	var wg sync.WaitGroup
	for i, name := range plan.Agents {
		wg.Add(1)
		go func(index int, agentName string) {
			defer wg.Done()
			results[index+1] = runSpecialist(ctx, specialistAgent{agentName, runnerForAgent(agentName)}, sessionID, phone, prompt)
		}(i, name)
	}
	wg.Wait()

	escalated, escalationReason := evaluateEscalation(results)
	if escalated {
		saveAgentTrace(sessionID, phone, "human-escalation", "required", time.Now(), escalationReason, nil)
		ensureEscalationTicket(sessionID, phone, escalationReason)
	}
	payload, _ := json.Marshal(results)
	supervisorPrompt := strings.Join([]string{"<user_message>", userMessage, "</user_message>", knowledgeContext, "<dispatch_plan>", string(planJSON), "</dispatch_plan>", "<main_draft>", draft, "</main_draft>", "<specialists>", string(payload), "</specialists>", "<human_escalation>", strconv.FormatBool(escalated), "</human_escalation>"}, "\n")
	started := time.Now()
	supervisorCtx, supervisorCancel := context.WithTimeout(ctx, 10*time.Second)
	finalAnswer, err := collectAgentReply(supervisorCtx, globalSupervisorRunner, []*schema.AgenticMessage{schema.UserAgenticMessage(supervisorPrompt)})
	supervisorCancel()
	if err != nil || strings.TrimSpace(finalAnswer) == "" {
		log.Printf("multi-agent supervisor degraded: session=%s err=%v", sessionID, err)
		saveAgentTrace(sessionID, phone, "supervisor", "degraded", started, draft, err)
		return strings.TrimSpace(draft), results
	}
	saveAgentTrace(sessionID, phone, "supervisor", "completed", started, finalAnswer, nil)
	return strings.TrimSpace(finalAnswer), results
}

func adminAgentTracesHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	if id, err := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64); err == nil && id > 0 {
		var trace AgentTrace
		if err := globalDB.First(&trace, uint(id)).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "agent trace not found", http.StatusNotFound)
			} else {
				http.Error(w, "failed to query agent trace", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"item": trace})
		return
	}
	limit := 100
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 500 {
		limit = value
	}
	query := globalDB.Order("created_at DESC").Limit(limit)
	if strings.TrimSpace(r.URL.Query().Get("compact")) == "1" {
		query = query.Select("id, session_id, patient_phone, agent_name, status, duration_ms, created_at")
	}
	if sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId")); sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}
	var traces []AgentTrace
	if err := query.Find(&traces).Error; err != nil {
		http.Error(w, "failed to query agent traces", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": traces, "count": len(traces)})
}
