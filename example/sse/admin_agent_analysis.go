package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

var adminAnalysisRoles = map[string]string{
	"Intent":      "你是意图识别分析器。自由分析输入的真实诉求、场景、隐含需求和需要追问的信息。",
	"Extractor":   "你是症状结构化提取器。自由提取主诉、部位、持续时间、程度、伴随症状、病史、用药与缺失信息。",
	"Risk":        "你是医疗风险分析器。自由分析危险信号、P1/P2/P3 风险等级、判断依据和安全提醒。",
	"Memory":      "你是病史与上下文分析器。自由分析信息之间的关联、前后矛盾、时间线和需要补充的既往史。",
	"Coordinator": "你是智能导诊主分析助手。像患者端 AI 一样自由理解输入，综合分析并给出清晰、谨慎、可执行的建议。",
	"Department":  "你是科室推荐分析器。自由分析首选科室、备选科室、推荐原因和就诊准备。",
	"Review":      "你是医疗安全复核助手。自由复核内容的医学安全性、遗漏风险、表达问题和需要修正的建议。",
}

func adminAgentAnalysisHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdminAPI(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if globalRunner == nil {
		http.Error(w, "agent is not ready", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Node    string `json:"node"`
		Message string `json:"message"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	instruction, ok := adminAnalysisRoles[strings.TrimSpace(req.Node)]
	if !ok || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "node and message are required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	prompt := instruction + "\n\n待分析内容：\n" + strings.TrimSpace(req.Message) + "\n\n请直接给出中文分析结果。医疗内容必须说明不能替代医生诊断。"
	sessionID := "admin-analysis-" + strings.ToLower(req.Node) + "-" + time.Now().Format("20060102150405.000")
	result, err := collectAgentReply(ctx, globalRunner, []*schema.AgenticMessage{schema.UserAgenticMessage(prompt)}, adk.WithSessionValues(map[string]any{"userID": "admin-analysis", "sessionID": sessionID}))
	if err != nil {
		http.Error(w, "analysis failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"node": req.Node, "content": result})
}
