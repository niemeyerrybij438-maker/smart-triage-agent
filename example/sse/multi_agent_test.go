package main

import (
	"testing"
)

func TestParseSpecialistResult(t *testing.T) {
	got := parseSpecialistResult("risk", `{"agent":"risk","riskLevel":"P2","summary":"??????","confidence":82}`)
	if got.Agent != "risk" || got.RiskLevel != "P2" || got.Confidence != 82 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseSpecialistResultFallback(t *testing.T) {
	got := parseSpecialistResult("intent", "temporary unavailable")
	if got.Agent != "intent" || got.Summary == "" {
		t.Fatalf("unexpected fallback: %+v", got)
	}
}

func TestBuildDispatchPlanMedication(t *testing.T) {
	plan := buildDispatchPlan("\u8fd9\u4e2a\u836f\u600e\u4e48\u6d82", specialistResult{Intent: "medication"})
	found := false
	for _, name := range plan.Agents {
		if name == "medication" {
			found = true
		}
	}
	if !found {
		t.Fatalf("medication agent missing: %+v", plan)
	}
}

func TestBuildDispatchPlanEmergency(t *testing.T) {
	plan := buildDispatchPlan("\u80f8\u75db\u800c\u4e14\u547c\u5438\u56f0\u96be", specialistResult{Intent: "emergency"})
	found := false
	for _, name := range plan.Agents {
		if name == "emergency" {
			found = true
		}
	}
	if !found {
		t.Fatalf("emergency agent missing: %+v", plan)
	}
}

func TestEvaluateEscalation(t *testing.T) {
	escalated, reason := evaluateEscalation([]specialistResult{{Agent: "risk", RiskLevel: "P1", Confidence: 90}})
	if !escalated || reason != "high_risk" {
		t.Fatalf("unexpected escalation: %v %s", escalated, reason)
	}
}

func TestBuildDispatchPlanIgnoresNegatedEmergencyAndMedicationTerms(t *testing.T) {
	plan := buildDispatchPlan("咳嗽两天，没有胸痛，也没有呼吸困难，无药物过敏", specialistResult{Intent: "symptom_triage", Summary: "安全提示包含胸痛、呼吸困难和用药风险"})
	for _, name := range plan.Agents {
		if name == "emergency" || name == "medication" {
			t.Fatalf("negated or assistant-summary terms must not dispatch %s: %+v", name, plan)
		}
	}
}

func TestBuildDispatchPlanDoesNotUseIntentSummaryKeywords(t *testing.T) {
	plan := buildDispatchPlan("咳嗽、喉咙痛，体温37.8℃", specialistResult{Intent: "symptom_triage", Summary: "未提及胸痛、呼吸困难，无药物过敏"})
	for _, name := range plan.Agents {
		if name == "emergency" || name == "medication" {
			t.Fatalf("intent summary polluted dispatch plan with %s: %+v", name, plan)
		}
	}
}
