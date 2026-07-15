package main

import "testing"

func TestRAGRetrievalEvaluationDataset(t *testing.T) {
	documents := []MedicalKnowledgeDocument{
		{Code: "EMERGENCY-CHEST-PAIN", Keywords: "\u80f8\u75db,\u80f8\u95f7,\u547c\u5438\u56f0\u96be,\u5598\u4e0d\u4e0a\u6c14,\u6025\u8bca"},
		{Code: "FEVER-TRIAGE", Keywords: "\u53d1\u70ed,\u53d1\u70e7,\u9ad8\u70ed,\u4f53\u6e29,\u76ae\u75b9,\u8131\u6c34"},
		{Code: "ABDOMINAL-PAIN", Keywords: "\u8179\u75db,\u809a\u5b50\u75bc,\u6076\u5fc3,\u5455\u5410,\u9ed1\u4fbf"},
		{Code: "SKIN-SYMPTOMS", Keywords: "\u76ae\u75b9,\u76ae\u80a4\u7619\u75d2,\u7ea2\u80bf,\u6c34\u6ce1,\u76ae\u80a4\u79d1"},
	}
	cases := []struct {
		name     string
		query    string
		expected string
	}{
		{name: "positive chest pain", query: "\u7a81\u7136\u80f8\u75db\uff0c\u800c\u4e14\u5598\u4e0d\u4e0a\u6c14", expected: "EMERGENCY-CHEST-PAIN"},
		{name: "high fever", query: "\u4f53\u6e29 39.2 \u5ea6\uff0c\u9ad8\u70ed\u4e24\u5929", expected: "FEVER-TRIAGE"},
		{name: "abdominal pain", query: "\u809a\u5b50\u75bc\uff0c\u8fd8\u6709\u6076\u5fc3\u5455\u5410", expected: "ABDOMINAL-PAIN"},
		{name: "skin symptoms", query: "\u76ae\u80a4\u7619\u75d2\u5e76\u4e14\u7ea2\u80bf\uff0c\u60f3\u770b\u76ae\u80a4\u79d1", expected: "SKIN-SYMPTOMS"},
	}
	passed := 0
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bestCode, bestScore := "", 0
			for _, document := range documents {
				if score := knowledgeScore(test.query, document); score > bestScore {
					bestCode, bestScore = document.Code, score
				}
			}
			if bestCode != test.expected || bestScore <= 0 {
				t.Fatalf("top document = %s score=%d, want %s", bestCode, bestScore, test.expected)
			}
			passed++
		})
	}
	t.Logf("RAG retrieval evaluation: %d/%d passed", passed, len(cases))
}

func TestAgentDispatchEvaluationDataset(t *testing.T) {
	cases := []struct {
		name      string
		message   string
		intent    string
		required  []string
		forbidden []string
	}{
		{name: "ordinary symptom", message: "\u666e\u901a\u54b3\u55fd\u4e24\u5929", intent: "triage", required: []string{"risk", "department", "history"}, forbidden: []string{"emergency", "medication"}},
		{name: "emergency signal", message: "\u7a81\u7136\u80f8\u75db\uff0c\u547c\u5438\u56f0\u96be", intent: "triage", required: []string{"emergency"}},
		{name: "negated emergency signal", message: "\u54b3\u55fd\u4e24\u5929\uff0c\u6ca1\u6709\u80f8\u75db\uff0c\u4e5f\u6ca1\u6709\u547c\u5438\u56f0\u96be", intent: "triage", forbidden: []string{"emergency"}},
		{name: "medication safety", message: "\u6211\u662f\u5b55\u5987\uff0c\u53ef\u4ee5\u5403\u4ec0\u4e48\u836f", intent: "medication", required: []string{"medication"}},
		{name: "child medication", message: "\u513f\u7ae5\u53d1\u70e7\u836f\u600e\u4e48\u5403", intent: "triage", required: []string{"medication"}},
	}
	passed := 0
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := buildDispatchPlan(test.message, specialistResult{Intent: test.intent})
			for _, name := range test.required {
				if !dispatchContains(plan.Agents, name) {
					t.Fatalf("dispatch plan %v is missing %s", plan.Agents, name)
				}
			}
			for _, name := range test.forbidden {
				if dispatchContains(plan.Agents, name) {
					t.Fatalf("dispatch plan %v must not contain %s", plan.Agents, name)
				}
			}
			passed++
		})
	}
	t.Logf("Agent dispatch evaluation: %d/%d passed", passed, len(cases))
}

func dispatchContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
