package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CoolBanHub/aggo/agent"
	agmsg "github.com/CoolBanHub/aggo/internal/agentic"
	"github.com/CoolBanHub/aggo/memory"
	"github.com/CoolBanHub/aggo/memory/builtin"
	"github.com/CoolBanHub/aggo/memory/builtin/storage"
	"github.com/CoolBanHub/aggo/model"
	"github.com/CoolBanHub/aggo/pkg/adapter"
	"github.com/CoolBanHub/aggo/pkg/sse"
	"github.com/CoolBanHub/aggo/utils"
	"github.com/cloudwego/eino/adk"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId,omitempty"`
}

type ChatResponse struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId"`
	Done      bool   `json:"done"`
}

var globalAgent adk.TypedAgent[*schema.AgenticMessage]
var globalRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalTriageExtractor adk.TypedAgent[*schema.AgenticMessage]
var globalTriageExtractorRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalReviewAgent adk.TypedAgent[*schema.AgenticMessage]
var globalReviewRunner *adk.TypedRunner[*schema.AgenticMessage]
var globalMemoryProvider memory.MemoryProvider
var cm einoModel.AgenticModel
var globalDB *gorm.DB
var triageExtractionMu sync.Mutex
var triageExtractionVersions = make(map[string]uint64)

func main() {
	loadEnvFiles()

	ctx := context.Background()

	if err := initializeBot(ctx); err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	defer globalMemoryProvider.Close()

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/doctor", doctorHandler)
	http.HandleFunc("/doctor/login", doctorLoginPageHandler)
	http.HandleFunc("/api/doctor/login", doctorLoginHandler)
	http.HandleFunc("/api/doctor/logout", doctorLogoutHandler)
	http.HandleFunc("/api/doctor/me", doctorMeHandler)
	http.HandleFunc("/api/chat", chatHandler)
	http.HandleFunc("/api/triage-records", triageRecordsHandler)
	http.HandleFunc("/api/triage-records/status", triageRecordStatusHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func getModelName() string {
	modelName := os.Getenv("Model")
	if modelName == "" {
		modelName = "gpt-5-nano"
	}
	return modelName
}

func loadEnvFiles() {
	for _, envFile := range []string{".env", "../.env", "../../.env"} {
		_ = godotenv.Load(envFile)
	}
}

func initializeBot(ctx context.Context) error {
	baseUrl := os.Getenv("BaseUrl")
	apiKey := os.Getenv("APIKey")
	if baseUrl == "" || apiKey == "" {
		return fmt.Errorf("BaseUrl and APIKey environment variables must be set")
	}
	var err error
	cm, err = model.NewChatModel(model.WithBaseUrl(baseUrl),
		model.WithAPIKey(apiKey),
		model.WithModel(getModelName()),
		model.WithMaxTokens(420),
	)
	if err != nil {
		return fmt.Errorf("new chat model fail,err:%s", err)
	}

	dbDSN := os.Getenv("MYSQL_DSN")
	if dbDSN == "" {
		dbDSN = "root:123456@tcp(127.0.0.1:3306)/aggo"
	}
	gormSql, err := NewMysqlGrom(dbDSN, logger.Silent)
	if err != nil {
		return fmt.Errorf("鍒涘缓鏁版嵁搴撹繛鎺ュけ璐? %v", err)
	}

	globalDB = gormSql
	if err := globalDB.AutoMigrate(&TriageRecord{}); err != nil {
		return fmt.Errorf("triage record table migration failed: %v", err)
	}
	displayName := doctorDisplayName()
	if displayName != "" {
		_ = globalDB.Model(&TriageRecord{}).Where("handled_by IN ?", []string{"doctor", "???"}).Update("handled_by", displayName).Error
	}
	log.Println("Triage record table is ready")

	s, err := storage.NewGormStorage(gormSql)
	if err != nil {
		return fmt.Errorf("new sql store fail,err:%s", err)
	}

	globalMemoryProvider, err = memory.GlobalRegistry().CreateProvider("builtin", &builtin.ProviderConfig{
		ChatModel: cm,
		Storage:   s,
		MemoryConfig: &builtin.MemoryConfig{
			EnableSessionSummary: false,
			EnableUserMemories:   false,
			MemoryLimit:          8,
			Retrieval:            builtin.RetrievalLastN,
		},
	})
	if err != nil {
		return fmt.Errorf("new manager fail,err:%s", err)
	}

	globalAgent, err = agent.NewAgentBuilder(cm).
		WithName("triage-assistant").
		WithDescription(triageDescription()).
		WithInstruction(triageInstruction()).
		WithMemory(globalMemoryProvider).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("new agent fail,err:%s", err)
	}

	globalRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: globalAgent})

	globalTriageExtractor, err = agent.NewAgentBuilder(cm).
		WithName("triage-record-extractor").
		WithDescription("\u5c06\u60a3\u8005\u5bf9\u8bdd\u6574\u7406\u4e3a\u533b\u751f\u7aef\u5bfc\u8bca\u8bb0\u5f55").
		WithInstruction(strings.Join([]string{
			"\u4f60\u662f\u5bfc\u8bca\u8bb0\u5f55\u63d0\u53d6\u5668\u3002\u6839\u636e\u60a3\u8005\u4e0e\u5bfc\u8bca\u52a9\u624b\u5bf9\u8bdd\uff0c\u8f93\u51fa\u4e00\u6761\u7ed3\u6784\u5316\u8bb0\u5f55\u3002",
			"\u53ea\u8f93\u51fa JSON \u5bf9\u8c61\uff0c\u4e0d\u8981 Markdown \u6216\u89e3\u91ca\u3002",
			`{"symptom":"patient symptom summary","riskLevel":"P1|P2|P3","riskText":"risk text","department":"primary department","alternatives":"alternative departments","reason":"reason","preparation":"preparation","warning":"warning"}`,
			"symptom \u53ea\u6c47\u603b\u60a3\u8005\u660e\u786e\u8bf4\u8fc7\u7684\u75c7\u72b6\u4e0e\u5173\u952e\u80cc\u666f\uff0c\u4e0d\u628a\u52a9\u624b\u7684\u8bdd\u5f53\u6210\u75c7\u72b6\u3002",
			"P1 \u4ec5\u7528\u4e8e\u660e\u786e\u6025\u75c7\uff1bP2 \u7528\u4e8e\u5efa\u8bae\u8f83\u5feb\u95e8\u8bca\uff1b\u8f7b\u5fae\u75c7\u72b6\u4e14\u65e0\u5371\u9669\u4fe1\u53f7\u901a\u5e38\u4e3a P3\u3002",
			"\u79d1\u5ba4\u4e0d\u786e\u5b9a\u65f6\u4f7f\u7528\u5168\u79d1\u533b\u5b66\u79d1\uff1b\u547c\u5438\u9053\u75c7\u72b6\u4f18\u5148\u547c\u5438\u5185\u79d1\u3002",
			"\u6587\u5b57\u7b80\u6d01\u4fdd\u5b88\uff0c\u4e0d\u505a\u786e\u8bca\uff0c\u4e0d\u7f16\u9020\u4fe1\u606f\u3002",
		}, "\n")).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("new triage extractor fail,err:%s", err)
	}
	globalTriageExtractorRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: globalTriageExtractor})

	globalReviewAgent, err = agent.NewAgentBuilder(cm).
		WithName("triage-answer-reviewer").
		WithDescription(reviewDescription()).
		WithInstruction(reviewInstruction()).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("new review agent fail,err:%s", err)
	}
	globalReviewRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: globalReviewAgent})

	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeHTMLPage(w, patientPage)
}

func writeTextAsOpenAIStream(writer *sse.Writer, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	response := adapter.MessageToOpenaiStreamResponse(agmsg.AssistantMessage(text), 0)
	if response == nil {
		return nil
	}
	return writer.WriteJSONData(response)
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = utils.GetULID()
	}
	ctx := r.Context()
	writer := sse.NewWriter(sessionID, w)
	if writer == nil {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	defer writer.Close()

	runOptions := []adk.AgentRunOption{adk.WithSessionValues(map[string]any{
		"userID": "sse-user", "sessionID": sessionID,
	})}

	if needsAnswerReview(req.Message) {
		draft, err := collectAgentReply(ctx, globalRunner, []*schema.AgenticMessage{schema.UserAgenticMessage(req.Message)}, runOptions...)
		if err != nil {
			log.Printf("reviewed triage draft failed: %v", err)
			_ = writeTextAsOpenAIStream(writer, "\u5bfc\u8bca\u670d\u52a1\u6682\u65f6\u65e0\u6cd5\u5b8c\u6210\u5224\u65ad\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002")
			_ = writer.WriteDone()
			return
		}
		finalAnswer := draft
		reviewCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		reviewed, review, reviewErr := reviewAnswer(reviewCtx, req.Message, draft)
		cancel()
		if reviewErr != nil {
			log.Printf("triage answer review degraded: %v", reviewErr)
		} else {
			finalAnswer = reviewed
			log.Printf("triage answer reviewed: session=%s approved=%t", sessionID, review.Approved)
		}
		if err := writeTextAsOpenAIStream(writer, finalAnswer); err != nil {
			return
		}
		_ = writer.WriteDone()
		if strings.TrimSpace(finalAnswer) != "" {
			version := nextTriageExtractionVersion(sessionID)
			go extractAndSaveTriageRecord(sessionID, req.Message, finalAnswer, version)
		}
		return
	}

	iter := globalRunner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage(req.Message)}, runOptions...)
	var assistantReply strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("Event error: %v", event.Err)
			break
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err != nil || msg == nil {
			continue
		}
		if agmsg.Text(msg) == "" && !agmsg.HasFunctionToolCall(msg) {
			continue
		}
		assistantReply.WriteString(agmsg.Text(msg))
		openaiResp := adapter.MessageToOpenaiStreamResponse(msg, 0)
		if openaiResp == nil {
			continue
		}
		if err := writer.WriteJSONData(openaiResp); err != nil {
			break
		}
	}
	_ = writer.WriteDone()
	if reply := strings.TrimSpace(assistantReply.String()); reply != "" {
		version := nextTriageExtractionVersion(sessionID)
		go extractAndSaveTriageRecord(sessionID, req.Message, reply, version)
	}
}
