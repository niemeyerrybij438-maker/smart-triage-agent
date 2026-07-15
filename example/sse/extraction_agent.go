package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	agmsg "github.com/CoolBanHub/aggo/internal/agentic"
	"github.com/CoolBanHub/aggo/memory"
	"github.com/cloudwego/eino/schema"
)

func normalizeExtractorJSON(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "\x60\x60\x60json")
	value = strings.TrimPrefix(value, "\x60\x60\x60")
	value = strings.TrimSuffix(value, "\x60\x60\x60")
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end > start {
		return value[start : end+1]
	}
	return value
}

func nextTriageExtractionVersion(sessionID string) uint64 {
	triageExtractionMu.Lock()
	defer triageExtractionMu.Unlock()
	triageExtractionVersions[sessionID]++
	return triageExtractionVersions[sessionID]
}

func isLatestTriageExtraction(sessionID string, version uint64) bool {
	triageExtractionMu.Lock()
	defer triageExtractionMu.Unlock()
	return triageExtractionVersions[sessionID] == version
}

func extractAndSaveTriageRecord(sessionID, patientPhone, userMessage, assistantReply, ragEvidence string, version uint64) {
	if globalTriageExtractorRunner == nil || globalDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	retrieved, err := globalMemoryProvider.Retrieve(ctx, &memory.RetrieveRequest{
		UserID:    "sse-user",
		SessionID: sessionID,
		Messages:  []*schema.AgenticMessage{schema.UserAgenticMessage(userMessage)},
		Limit:     8,
	})
	if err != nil {
		log.Printf("retrieve triage conversation failed: %v", err)
	}

	conversation := make([]string, 0, 12)
	if retrieved != nil {
		for _, msg := range retrieved.HistoryMessages {
			text := strings.TrimSpace(agmsg.Text(msg))
			if text == "" {
				continue
			}
			role := "\u60a3\u8005"
			if msg.Role == schema.AgenticRoleTypeAssistant {
				role = "\u5bfc\u8bca\u52a9\u624b"
			}
			conversation = append(conversation, role+"\uff1a"+text)
		}
	}
	conversation = append(conversation, "\u60a3\u8005\uff1a"+strings.TrimSpace(userMessage), "\u5bfc\u8bca\u52a9\u624b\uff1a"+strings.TrimSpace(assistantReply))
	prompt := "\u8bf7\u5c06\u4ee5\u4e0b\u4f1a\u8bdd\u6574\u7406\u4e3a\u533b\u751f\u7aef\u5bfc\u8bca\u8bb0\u5f55\uff1a\n" + strings.Join(conversation, "\n")

	iter := globalTriageExtractorRunner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage(prompt)})
	var output strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("triage extractor event failed: %v", event.Err)
			return
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err == nil && msg != nil {
			output.WriteString(agmsg.Text(msg))
		}
	}

	var extracted SaveTriageRecordRequest
	if err := json.Unmarshal([]byte(normalizeExtractorJSON(output.String())), &extracted); err != nil {
		log.Printf("parse triage extractor output failed: %v, output=%q", err, output.String())
		return
	}
	extracted.SessionID = sessionID
	extracted.PatientPhone = patientPhone
	extracted.PatientProfile = patientProfileSnapshotJSON(patientPhone)
	extracted.RAGEvidence = strings.TrimSpace(ragEvidence)
	if strings.TrimSpace(extracted.Symptom) == "" || strings.TrimSpace(extracted.Department) == "" {
		return
	}
	if extracted.Status == "" {
		extracted.Status = "pending"
	}
	if !isLatestTriageExtraction(sessionID, version) {
		return
	}
	if err := upsertTriageRecord(extracted); err != nil {
		log.Printf("save extracted triage record failed: %v", err)
		return
	}
}
