package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	triageReviewMaxRounds = 2
	triageRunTimeout      = 45 * time.Second
	triageDraftTimeout    = 12 * time.Second
	triageReviewTimeout   = 10 * time.Second
)

type triageHarnessInput struct {
	SessionID    string
	PatientPhone string
	UserMessage  string
}

type triageHarnessResult struct {
	Answer           string
	HealthContext    string
	KnowledgeHits    []medicalKnowledgeHit
	KnowledgeContext string
}

// triageHarness owns one complete triage run: context assembly, RAG, agent orchestration,
// AI review and the bounded convergence loop. HTTP handlers only handle transport concerns.
type triageHarness struct {
	maxReviewRounds int
}

func newTriageHarness() triageHarness {
	return triageHarness{maxReviewRounds: triageReviewMaxRounds}
}

func (h triageHarness) Run(ctx context.Context, input triageHarnessInput) triageHarnessResult {
	runCtx, cancel := context.WithTimeout(ctx, triageRunTimeout)
	defer cancel()

	healthContext := patientProfileAIContext(input.PatientPhone)
	knowledgeStarted := time.Now()
	ragQuery, ragContextSummary := buildRAGRetrievalContext(runCtx, input)
	knowledgeRetrieval := retrieveMedicalKnowledgeWithAudit(ragQuery+" "+healthContext, 3)
	knowledgeRetrieval.CurrentInput = strings.TrimSpace(input.UserMessage)
	knowledgeRetrieval.ContextSummary = ragContextSummary
	knowledgeHits := knowledgeRetrieval.Hits
	knowledgeContext := formatMedicalKnowledgeContext(knowledgeHits)
	knowledgeSummary, _ := json.Marshal(knowledgeRetrieval)
	knowledgeStatus := "completed"
	if len(knowledgeHits) == 0 {
		knowledgeStatus = "no_match"
	}
	saveAgentTrace(input.SessionID, input.PatientPhone, "medical-knowledge-rag", knowledgeStatus, knowledgeStarted, string(knowledgeSummary), nil)

	contextParts := make([]string, 0, 2)
	if healthContext != "" {
		contextParts = append(contextParts, healthContext)
	}
	if knowledgeContext != "" {
		contextParts = append(contextParts, knowledgeContext)
	}
	agentMessage := input.UserMessage
	if len(contextParts) > 0 {
		agentMessage = strings.Join(contextParts, "\n") + "\n<current_complaint>\n" + input.UserMessage + "\n</current_complaint>"
	}

	runOptions := []adk.AgentRunOption{adk.WithSessionValues(map[string]any{"userID": input.PatientPhone, "sessionID": input.SessionID})}
	draftStarted := time.Now()
	var draft string
	var draftErr error
	var intent specialistResult
	var initialWait sync.WaitGroup
	initialWait.Add(2)
	go func() {
		defer initialWait.Done()
		draftCtx, draftCancel := context.WithTimeout(runCtx, triageDraftTimeout)
		draft, draftErr = collectAgentReply(draftCtx, globalRunner, []*schema.AgenticMessage{schema.UserAgenticMessage(agentMessage)}, runOptions...)
		draftCancel()
		if draftErr != nil {
			draft = fallbackTriageReply(input.UserMessage)
			saveAgentTrace(input.SessionID, input.PatientPhone, "main-triage", "degraded", draftStarted, draft, draftErr)
		} else {
			saveAgentTrace(input.SessionID, input.PatientPhone, "main-triage", "completed", draftStarted, draft, nil)
		}
	}()
	go func() {
		defer initialWait.Done()
		intentPrompt := buildSpecialistPrompt(input.UserMessage, healthContext, knowledgeContext, "")
		intent = runSpecialist(runCtx, specialistAgent{"intent", globalIntentRunner}, input.SessionID, input.PatientPhone, intentPrompt)
	}()
	initialWait.Wait()

	answer, _ := orchestrateTriage(runCtx, input.SessionID, input.PatientPhone, input.UserMessage, healthContext, knowledgeContext, draft, intent)
	if needsAnswerReview(input.UserMessage) {
		answer = h.runReviewLoop(runCtx, input, knowledgeContext, answer)
	} else {
		saveAgentTrace(input.SessionID, input.PatientPhone, "ai-review", "approved_rules", time.Now(), "低风险常规导诊通过规则审核", nil)
	}
	return triageHarnessResult{Answer: strings.TrimSpace(answer), HealthContext: healthContext, KnowledgeHits: knowledgeHits, KnowledgeContext: knowledgeContext}
}

func (h triageHarness) runReviewLoop(ctx context.Context, input triageHarnessInput, knowledgeContext, answer string) string {
	candidate := strings.TrimSpace(answer)
	for round := 1; round <= h.maxReviewRounds; round++ {
		started := time.Now()
		reviewCtx, cancel := context.WithTimeout(ctx, triageReviewTimeout)
		reviewed, result, err := reviewAnswerWithKnowledge(reviewCtx, input.UserMessage, knowledgeContext, candidate)
		cancel()
		if err != nil {
			saveAgentTrace(input.SessionID, input.PatientPhone, "ai-review", "degraded", started, candidate, err)
			return candidate
		}
		if result.Approved {
			saveAgentTrace(input.SessionID, input.PatientPhone, "ai-review", "approved", started, result.Reason, nil)
			return candidate
		}
		candidate = strings.TrimSpace(reviewed)
		status := "corrected"
		if round == h.maxReviewRounds {
			status = "corrected_max_rounds"
		}
		summary, _ := json.Marshal(map[string]any{"round": round, "reason": result.Reason})
		saveAgentTrace(input.SessionID, input.PatientPhone, "ai-review", status, started, string(summary), nil)
	}
	return candidate
}
