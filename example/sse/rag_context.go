package main

import (
	"context"
	"encoding/json"
	"strings"
)

type ragConversationMessage struct {
	Role string
	Text string
}

var ragClinicalTerms = []string{
	"严重呼吸困难", "呼吸困难", "喘不上气", "胸口压迫感", "单侧肢体无力", "意识模糊", "意识异常", "喉咙肿", "口唇发紫",
	"喉咙痛", "咽痛", "高热", "发热", "发烧", "咳嗽", "干咳", "胸痛", "胸闷", "晕厥", "昏倒", "头痛", "腹痛", "肚子痛", "胃痛",
	"呕吐", "腹泻", "便血", "黑便", "呕血", "抽搐", "皮疹", "荨麻疹", "过敏", "流鼻涕", "鼻塞", "痰", "乏力", "怀孕",
}

func buildRAGRetrievalContext(ctx context.Context, input triageHarnessInput) (string, string) {
	messages := loadRAGConversation(ctx, input)
	summary := summarizeRAGConversation(messages, input.UserMessage)
	if strings.TrimSpace(summary) == "" {
		summary = strings.TrimSpace(input.UserMessage)
	}
	return summary, summary
}

func loadRAGConversation(ctx context.Context, input triageHarnessInput) []ragConversationMessage {
	_ = ctx
	return deduplicateRAGMessages(loadRAGConversationFromPatientChat(input))
}

func loadRAGConversationFromPatientChat(input triageHarnessInput) []ragConversationMessage {
	if globalDB == nil || strings.TrimSpace(input.SessionID) == "" {
		return nil
	}
	var chat PatientChat
	if globalDB.Where("patient_phone = ? AND session_id = ?", input.PatientPhone, input.SessionID).Order("updated_at DESC").First(&chat).Error != nil {
		return nil
	}
	var payload []struct {
		User bool   `json:"user"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(chat.MessagesJSON), &payload) != nil {
		return nil
	}
	messages := make([]ragConversationMessage, 0, len(payload))
	for _, item := range payload {
		text := strings.TrimSpace(item.Text)
		if text == "" || strings.Contains(text, "正在进行多 Agent") {
			continue
		}
		role := "assistant"
		if item.User {
			role = "user"
		}
		messages = append(messages, ragConversationMessage{Role: role, Text: text})
	}
	return messages
}

func deduplicateRAGMessages(messages []ragConversationMessage) []ragConversationMessage {
	result := make([]ragConversationMessage, 0, len(messages))
	for _, message := range messages {
		if len(result) > 0 && result[len(result)-1].Role == message.Role && result[len(result)-1].Text == message.Text {
			continue
		}
		result = append(result, message)
	}
	if len(result) > 8 {
		result = result[len(result)-8:]
	}
	return result
}

func summarizeRAGConversation(messages []ragConversationMessage, current string) string {
	conversation := append([]ragConversationMessage(nil), messages...)
	current = strings.TrimSpace(current)
	if current != "" && (len(conversation) == 0 || conversation[len(conversation)-1].Role != "user" || conversation[len(conversation)-1].Text != current) {
		conversation = append(conversation, ragConversationMessage{Role: "user", Text: current})
	}
	states := map[string]bool{}
	stateSeen := map[string]bool{}
	recent := make([]string, 0, 4)
	lastAssistant := ""
	for _, message := range conversation {
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		if message.Role == "assistant" {
			lastAssistant = text
			continue
		}
		normalized := normalizeKnowledgeText(text)
		if isShortNegativeReply(normalized) && lastAssistant != "" {
			for _, term := range assistantQuestionTerms(lastAssistant) {
				states[term] = false
				stateSeen[term] = true
			}
		} else if isShortPositiveReply(normalized) && lastAssistant != "" {
			for _, term := range assistantQuestionTerms(lastAssistant) {
				states[term] = true
				stateSeen[term] = true
			}
		}
		for _, term := range termsInText(text) {
			states[term] = !keywordNegated(normalized, normalizeKnowledgeText(term))
			stateSeen[term] = true
		}
		if !isBareShortReply(normalized) {
			recent = appendUniqueRecent(recent, text, 4)
		}
	}
	positive, negative := make([]string, 0), make([]string, 0)
	for _, term := range ragClinicalTerms {
		if !stateSeen[term] {
			continue
		}
		if states[term] {
			positive = append(positive, term)
		} else {
			negative = append(negative, term)
		}
	}
	parts := make([]string, 0, 3)
	if len(positive) > 0 {
		parts = append(parts, "当前症状："+strings.Join(positive, "、"))
	}
	if len(negative) > 0 {
		parts = append(parts, "当前否认："+strings.Join(negative, "、"))
	}
	if len(recent) > 0 {
		parts = append(parts, "近期患者陈述："+strings.Join(recent, "；"))
	}
	return strings.Join(parts, "。")
}

func termsInText(value string) []string {
	normalized := normalizeKnowledgeText(value)
	terms := make([]string, 0, 4)
	for _, term := range ragClinicalTerms {
		if strings.Contains(normalized, normalizeKnowledgeText(term)) {
			terms = append(terms, term)
		}
	}
	return terms
}

func assistantQuestionTerms(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimRight(value, " \t\r\n")
	end := strings.LastIndexAny(value, "？?")
	if end < 0 {
		return nil
	}
	question := value[:end]
	if start := strings.LastIndexAny(question, "。！？!?；;\n"); start >= 0 {
		question = question[start+1:]
	}
	question = strings.TrimSpace(question)
	if question == "" || !containsAny(question, "吗", "么", "是否", "有无", "有没有", "什么", "多久", "几天") {
		return nil
	}
	return termsInText(question)
}

func isShortNegativeReply(value string) bool {
	value = strings.ReplaceAll(value, " ", "")
	return value == "没有" || value == "无" || value == "否" || value == "不是" || value == "没有的" || value == "都没有"
}

func isShortPositiveReply(value string) bool {
	value = strings.ReplaceAll(value, " ", "")
	return value == "有" || value == "是" || value == "有的" || value == "是的"
}

func isBareShortReply(value string) bool {
	return len([]rune(strings.ReplaceAll(value, " ", ""))) <= 4 && (isShortNegativeReply(value) || isShortPositiveReply(value))
}

func appendUniqueRecent(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 80 {
		value = string([]rune(value)[:80])
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}
