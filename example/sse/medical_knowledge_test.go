package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestKnowledgeScoreMatchesChineseEmergencyTerms(t *testing.T) {
	document := MedicalKnowledgeDocument{
		Title:    "Chest pain emergency triage",
		Keywords: "chest pain,shortness of breath,\u80f8\u75db,\u547c\u5438\u56f0\u96be,\u6025\u8bca",
		Content:  "Severe symptoms require emergency assessment.",
	}
	if score := knowledgeScore("\u7a81\u7136\u80f8\u75db\u800c\u4e14\u547c\u5438\u56f0\u96be", document); score <= 0 {
		t.Fatalf("knowledgeScore() = %d, want positive score", score)
	}
}

func TestFormatMedicalKnowledgeContextIncludesSourceAndBoundary(t *testing.T) {
	context := formatMedicalKnowledgeContext([]medicalKnowledgeHit{{
		Code:       "TEST",
		Title:      "Test guidance",
		Content:    "Triage guidance only.",
		SourceName: "Public source",
		SourceURL:  "https://example.org/guidance",
	}})
	for _, expected := range []string{"<medical_knowledge_references>", "do not diagnose", "Public source", "https://example.org/guidance"} {
		if !containsAny(context, expected) {
			t.Fatalf("context does not contain %q: %q", expected, context)
		}
	}
}

func TestReviewLoopHasBoundedRounds(t *testing.T) {
	if triageReviewMaxRounds != 2 {
		t.Fatalf("triageReviewMaxRounds = %d, want 2", triageReviewMaxRounds)
	}
	if harness := newTriageHarness(); harness.maxReviewRounds != triageReviewMaxRounds {
		t.Fatalf("harness maxReviewRounds = %d", harness.maxReviewRounds)
	}
}

func TestSeedMigrationDoesNotOverwriteEditedKnowledge(t *testing.T) {
	reviewTime := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	createdAt := reviewTime.Add(-time.Hour)
	seeded := MedicalKnowledgeDocument{Title: "呼吸道症状分诊", Content: "知识内容", Keywords: "咳嗽", SourceName: "世界卫生组织", SourceURL: "https://example.org", ReviewStatus: "approved", Version: 1}
	existing := MedicalKnowledgeDocument{Title: "old", ReviewStatus: "approved", Version: 1, CreatedAt: createdAt}
	migrated, changed := applySeededKnowledgeMigration(existing, seeded, reviewTime)
	if !changed || migrated.ReviewedBy != "系统预置审核" || migrated.ReviewedAt == nil || !migrated.ReviewedAt.Equal(createdAt) {
		t.Fatalf("unexpected migrated metadata: changed=%v reviewer=%q reviewedAt=%v", changed, migrated.ReviewedBy, migrated.ReviewedAt)
	}
	migratedAgain, changedAgain := applySeededKnowledgeMigration(migrated, seeded, reviewTime.Add(time.Hour))
	if changedAgain || migratedAgain.ReviewedAt == nil || !migratedAgain.ReviewedAt.Equal(createdAt) {
		t.Fatal("restarting must not rewrite an already migrated seed or its review time")
	}
	edited := migrated
	edited.Version = 2
	edited.Title = "管理员编辑内容"
	preserved, editedChanged := applySeededKnowledgeMigration(edited, seeded, reviewTime)
	if editedChanged || preserved.Title != edited.Title {
		t.Fatal("edited version 2 knowledge must not be overwritten at startup")
	}
}

func TestRAGRetrievalReadsApprovedEnabledKnowledgeOnly(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	rows := sqlmock.NewRows([]string{"id", "code", "title", "content", "keywords", "source_name", "source_url", "enabled", "review_status", "version"}).
		AddRow(1, "RESPIRATORY-TRIAGE", "呼吸道症状分诊", "咳嗽需要询问病程和呼吸状态。", "咳嗽,呼吸困难", "世界卫生组织", "https://example.org/respiratory", true, "approved", 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `medical_knowledge_documents` WHERE enabled = ? AND review_status = ?")).
		WithArgs(true, "approved").WillReturnRows(rows)
	hits := retrieveMedicalKnowledge("咳嗽两天，呼吸有点困难", 3)
	if len(hits) != 1 || hits[0].Code != "RESPIRATORY-TRIAGE" {
		t.Fatalf("unexpected RAG hits: %#v", hits)
	}
	context := formatMedicalKnowledgeContext(hits)
	for _, marker := range []string{"<medical_knowledge_references>", "RESPIRATORY-TRIAGE", "世界卫生组织"} {
		if !containsAny(context, marker) {
			t.Fatalf("RAG context missing %q: %s", marker, context)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("RAG retrieval issued unexpected database operations: %v", err)
	}
}

func TestRAGEvidenceSnapshotContainsImmutableAuditFields(t *testing.T) {
	reviewedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	retrievedAt := reviewedAt.Add(time.Hour)
	hit := medicalKnowledgeHit{Code: "RESPIRATORY-TRIAGE", Title: "呼吸道症状分诊", Content: "当时使用的知识内容", SourceName: "世界卫生组织", SourceURL: "https://example.org", Version: 3, Score: 8, ReviewedBy: "审核员", ReviewedAt: &reviewedAt, RetrievedAt: retrievedAt}
	encoded, err := json.Marshal([]medicalKnowledgeHit{hit})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []medicalKnowledgeHit
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].Version != 3 || snapshot[0].Content != hit.Content || snapshot[0].Score != 8 || snapshot[0].ReviewedBy != "审核员" || !snapshot[0].RetrievedAt.Equal(retrievedAt) {
		t.Fatalf("incomplete RAG evidence snapshot: %#v", snapshot)
	}
	if got := ragEvidenceSourceSummary(string(encoded)); !containsAny(got, "呼吸道症状分诊 v3", "世界卫生组织") {
		t.Fatalf("unexpected source summary: %q", got)
	}
}

func TestNormalizeLegacyRAGEvidenceAddsVersionAndRetrievalTime(t *testing.T) {
	traceTime := time.Date(2026, 7, 14, 10, 30, 0, 0, time.UTC)
	summary := `[{"code":"RESPIRATORY-TRIAGE","title":"呼吸道症状分诊","content":"历史快照","sourceName":"世界卫生组织","score":7}]`
	normalized, ok := normalizeLegacyRAGEvidence(summary, traceTime)
	if !ok {
		t.Fatal("legacy RAG trace should be recoverable")
	}
	var rows []medicalKnowledgeHit
	if json.Unmarshal([]byte(normalized), &rows) != nil || len(rows) != 1 || rows[0].Version != 1 || !rows[0].RetrievedAt.Equal(traceTime) || rows[0].Content != "历史快照" {
		t.Fatalf("unexpected normalized legacy evidence: %s", normalized)
	}
	if _, ok := normalizeLegacyRAGEvidence(`{"not":"an array"}`, traceTime); ok {
		t.Fatal("invalid legacy trace must not be backfilled")
	}
}

func TestNegatedSymptomsDoNotPromoteEmergencyKnowledge(t *testing.T) {
	chest := MedicalKnowledgeDocument{Code: "EMERGENCY-CHEST-PAIN", Title: "胸痛与呼吸困难急诊分诊", Keywords: "胸痛,呼吸困难,晕厥,急诊"}
	score, reasons, excluded := knowledgeMatchEvaluation(normalizeKnowledgeText("咳嗽两天，没有胸痛，也没有明显呼吸困难"), chest)
	if score != 0 || len(reasons) != 0 {
		t.Fatalf("negated emergency symptoms must not score: score=%d reasons=%v", score, reasons)
	}
	joined := strings.Join(excluded, " ")
	for _, marker := range []string{"否定症状“胸痛”", "否定症状“呼吸困难”", "缺少正向危险信号"} {
		if !strings.Contains(joined, marker) {
			t.Fatalf("missing exclusion reason %q in %v", marker, excluded)
		}
	}
	respiratory := MedicalKnowledgeDocument{Code: "RESPIRATORY-TRIAGE", Title: "呼吸道症状分诊", Keywords: "咳嗽,咽痛,呼吸困难"}
	respiratoryScore, respiratoryReasons, respiratoryExcluded := knowledgeMatchEvaluation(normalizeKnowledgeText("咳嗽、咽痛两天，没有呼吸困难"), respiratory)
	if respiratoryScore <= 0 || len(respiratoryReasons) < 2 || !containsAny(strings.Join(respiratoryExcluded, " "), "否定症状“呼吸困难”") {
		t.Fatalf("positive respiratory symptoms should remain while negated danger signal is excluded: score=%d reasons=%v excluded=%v", respiratoryScore, respiratoryReasons, respiratoryExcluded)
	}
}

func TestRAGAuditExcludesNegatedChestPainFromTopResults(t *testing.T) {
	db, mock := newMockGormDB(t)
	useGlobalDBForTest(t, db)
	rows := sqlmock.NewRows([]string{"id", "code", "title", "content", "keywords", "source_name", "source_url", "enabled", "review_status", "version"}).
		AddRow(1, "EMERGENCY-CHEST-PAIN", "胸痛与呼吸困难急诊分诊", "急诊知识", "胸痛,呼吸困难,晕厥,急诊", "MedlinePlus", "https://example.org/chest", true, "approved", 1).
		AddRow(2, "RESPIRATORY-TRIAGE", "呼吸道症状分诊", "呼吸道知识", "咳嗽,咽痛,呼吸困难", "世界卫生组织", "https://example.org/resp", true, "approved", 1).
		AddRow(3, "DEIDENTIFIED-CASE-RESPIRATORY", "脱敏导诊案例", "案例知识", "咳嗽,咽痛,成人,呼吸困难", "教学案例", "https://example.org/case", true, "approved", 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `medical_knowledge_documents` WHERE enabled = ? AND review_status = ?")).WithArgs(true, "approved").WillReturnRows(rows)
	audit := retrieveMedicalKnowledgeWithAudit("咳嗽、咽痛两天，没有胸痛，也没有明显呼吸困难", 3)
	for _, hit := range audit.Hits {
		if hit.Code == "EMERGENCY-CHEST-PAIN" {
			t.Fatalf("negated chest pain knowledge entered hits: %#v", audit.Hits)
		}
	}
	if len(audit.Hits) != 2 || len(audit.Excluded) == 0 || audit.Excluded[0].Code != "EMERGENCY-CHEST-PAIN" {
		t.Fatalf("unexpected retrieval audit: %#v", audit)
	}
}

func TestRAGConversationContextCarriesSymptomsAcrossShortReplies(t *testing.T) {
	messages := []ragConversationMessage{
		{Role: "user", Text: "我咳嗽、喉咙痛，体温37.8℃"},
		{Role: "assistant", Text: "这些症状持续多久了？"},
	}
	summary := summarizeRAGConversation(messages, "两天了")
	for _, marker := range []string{"当前症状：", "咳嗽", "喉咙痛", "两天了"} {
		if !strings.Contains(summary, marker) {
			t.Fatalf("multi-turn RAG summary missing %q: %s", marker, summary)
		}
	}
}

func TestRAGConversationContextLinksNegativeShortReplyToQuestion(t *testing.T) {
	messages := []ragConversationMessage{
		{Role: "user", Text: "我咳嗽两天"},
		{Role: "assistant", Text: "有没有胸痛或明显呼吸困难？"},
	}
	summary := summarizeRAGConversation(messages, "没有")
	for _, marker := range []string{"当前症状：咳嗽", "当前否认：", "胸痛", "呼吸困难"} {
		if !strings.Contains(summary, marker) {
			t.Fatalf("negative reply was not linked to prior question, missing %q: %s", marker, summary)
		}
	}
	chest := MedicalKnowledgeDocument{Code: "EMERGENCY-CHEST-PAIN", Keywords: "胸痛,呼吸困难,急诊"}
	if score := knowledgeScore(summary, chest); score != 0 {
		t.Fatalf("linked negative reply promoted emergency knowledge: score=%d summary=%s", score, summary)
	}
}

func TestRAGConversationContextAppliesSymptomCorrection(t *testing.T) {
	messages := []ragConversationMessage{{Role: "user", Text: "我一直咳嗽"}}
	summary := summarizeRAGConversation(messages, "不是咳嗽，是胃痛")
	if !strings.Contains(summary, "当前症状：胃痛") || !strings.Contains(summary, "当前否认：咳嗽") {
		t.Fatalf("symptom correction was not applied: %s", summary)
	}
}

func TestRAGConversationContextDoesNotTreatAssistantSafetyAdviceAsSymptoms(t *testing.T) {
	messages := []ragConversationMessage{
		{Role: "user", Text: "我咳嗽、喉咙痛，体温37.8℃"},
		{Role: "assistant", Text: "若出现胸痛、呼吸困难、晕厥请立即急诊。你有胸痛吗？"},
	}
	summary := summarizeRAGConversation(messages, "两天了，没有胸痛")
	for _, marker := range []string{"咳嗽", "喉咙痛", "两天了", "当前否认：胸痛"} {
		if !strings.Contains(summary, marker) {
			t.Fatalf("RAG summary missing %q: %s", marker, summary)
		}
	}
	for _, polluted := range []string{"当前症状：呼吸困难", "当前症状：晕厥", "当前症状：胸痛", "、呼吸困难", "、晕厥"} {
		if strings.Contains(summary, polluted) {
			t.Fatalf("assistant safety advice polluted RAG summary with %q: %s", polluted, summary)
		}
	}
	chest := MedicalKnowledgeDocument{Code: "EMERGENCY-CHEST-PAIN", Keywords: "胸痛,呼吸困难,晕厥,急诊"}
	if score := knowledgeScore(summary, chest); score != 0 {
		t.Fatalf("assistant safety advice promoted emergency knowledge: score=%d summary=%s", score, summary)
	}
	respiratory := MedicalKnowledgeDocument{Code: "RESPIRATORY-TRIAGE", Keywords: "咳嗽,咽痛,喉咙痛,呼吸困难"}
	if score := knowledgeScore(summary, respiratory); score <= 0 {
		t.Fatalf("patient respiratory symptoms should still match: score=%d summary=%s", score, summary)
	}
}

func TestAssistantQuestionTermsOnlyUsesFinalExplicitQuestion(t *testing.T) {
	terms := assistantQuestionTerms("若出现胸痛、呼吸困难、晕厥请立即急诊。你现在有胸痛吗？")
	joined := strings.Join(terms, "、")
	if joined != "胸痛" {
		t.Fatalf("unexpected assistant question terms: %v", terms)
	}
}
