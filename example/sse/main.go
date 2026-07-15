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
	startFollowUpNotificationLoop()
	startPersistentSessionCleanupLoop()

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/ready", readinessHandler)
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/patient/login", patientLoginPageHandler)
	http.HandleFunc("/doctor", doctorHandler)
	http.HandleFunc("/admin", adminHandler)
	http.HandleFunc("/admin/login", adminLoginPageHandler)
	http.HandleFunc("/doctor/login", doctorLoginPageHandler)
	http.HandleFunc("/api/doctor/login", doctorLoginHandler)
	http.HandleFunc("/api/doctor/logout", doctorLogoutHandler)
	http.HandleFunc("/api/doctor/me", doctorMeHandler)
	http.HandleFunc("/api/admin/login", adminLoginHandler)
	http.HandleFunc("/api/admin/logout", adminLogoutHandler)
	http.HandleFunc("/api/admin/me", adminMeHandler)
	http.HandleFunc("/api/admin/dashboard", adminDashboardHandler)
	http.HandleFunc("/api/admin/follow-ups", adminFollowUpDetailsHandler)
	http.HandleFunc("/api/admin/follow-ups/export", adminFollowUpExportHandler)
	http.HandleFunc("/api/admin/doctors", adminDoctorsHandler)
	http.HandleFunc("/api/admin/doctors/update", adminDoctorUpdateHandler)
	http.HandleFunc("/api/admin/doctors/reset-password", adminDoctorResetPasswordHandler)
	http.HandleFunc("/api/admin/audit", adminAuditHandler)
	http.HandleFunc("/api/admin/records", adminRecordsHandler)
	http.HandleFunc("/api/admin/password", adminPasswordHandler)
	http.HandleFunc("/api/admin/platform-config", adminPlatformConfigHandler)
	http.HandleFunc("/api/admin/risk-rules", adminRiskRulesHandler)
	http.HandleFunc("/api/admin/risk-rules/update", adminRiskRuleUpdateHandler)
	http.HandleFunc("/api/admin/agent-analysis", adminAgentAnalysisHandler)
	http.HandleFunc("/api/admin/agent-traces", adminAgentTracesHandler)
	http.HandleFunc("/api/admin/medical-knowledge", adminMedicalKnowledgeHandler)
	http.HandleFunc("/api/chat", chatHandler)
	http.HandleFunc("/api/triage-records", triageRecordsHandler)
	http.HandleFunc("/api/triage-records/status", triageRecordStatusHandler)
	http.HandleFunc("/api/escalation-tickets", escalationTicketsHandler)
	http.HandleFunc("/api/follow-ups", followUpsHandler)
	http.HandleFunc("/api/follow-ups/feedback", followUpFeedbackHandler)
	http.HandleFunc("/api/follow-up-notifications", followUpNotificationsHandler)
	http.HandleFunc("/api/patient/sms", patientSMSHandler)
	http.HandleFunc("/api/patient/login", patientLoginHandler)
	http.HandleFunc("/api/patient/me", patientMeHandler)
	http.HandleFunc("/api/patient/profile", patientProfileHandler)
	http.HandleFunc("/api/patient/chats", patientChatsHandler)
	http.HandleFunc("/api/patient/logout", patientLogoutHandler)
	http.HandleFunc("/api/map/config", mapConfigHandler)
	http.HandleFunc("/api/appointments", appointmentsHandler)
	http.HandleFunc("/api/appointments/status", appointmentStatusHandler)
	http.HandleFunc("/api/hospitals/nearby", hospitalLocationsHandler)
	http.HandleFunc("/api/reports/triage.pdf", triageReportPDFHandler)

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
	if err := validateRuntimeConfiguration(); err != nil {
		return err
	}
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

	dbDSN := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	gormSql, err := NewMysqlGrom(dbDSN, logger.Silent)
	if err != nil {
		return fmt.Errorf("database connection failed: %v", err)
	}

	globalDB = gormSql
	migrationModels := []any{&TriageRecord{}, &StaffUser{}, &AuditLog{}, &RiskRule{}, &AgentTrace{}, &PatientChat{}, &EscalationTicket{}, &PlatformConfig{}, &MedicalKnowledgeDocument{}, &FollowUpPlan{}, &FollowUpNotification{}, &PersistentSession{}}
	migrationModels = append(migrationModels, patientServicesModels()...)
	if err := globalDB.AutoMigrate(migrationModels...); err != nil {
		return fmt.Errorf("application table migration failed: %v", err)
	}
	if err := migrateLegacyFollowUpNotificationReadState(); err != nil {
		return fmt.Errorf("follow-up notification migration failed: %v", err)
	}
	cleanupExpiredPersistentSessions()
	if err := seedRiskRules(); err != nil {
		return fmt.Errorf("risk rule initialization failed: %v", err)
	}
	if err := seedMedicalKnowledge(); err != nil {
		return fmt.Errorf("medical knowledge initialization failed: %v", err)
	}
	if err := backfillRAGEvidenceSnapshots(); err != nil {
		log.Printf("RAG evidence backfill skipped: %v", err)
	}

	if err := seedStaffAccounts(); err != nil {
		return fmt.Errorf("staff account initialization failed: %v", err)
	}
	reconcileEscalationPriorities()
	refreshEscalationSLAs()
	refreshFollowUpStatuses()
	if err := reconcileClosedEscalationFollowUps(); err != nil {
		log.Printf("closed follow-up escalation reconciliation failed: %v", err)
	}
	if err := generateFollowUpNotifications(time.Now()); err != nil {
		log.Printf("follow-up notification initialization failed: %v", err)
	}
	reconcileAppointmentHospitals()
	displayName := doctorDisplayName()
	if displayName != "" {
		_ = globalDB.Model(&TriageRecord{}).Where("handled_by IN ?", []string{"doctor"}).Update("handled_by", displayName).Error
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

	intentAgent, buildErr := agent.NewAgentBuilder(cm).WithName("intent-agent").WithDescription("??????????").WithInstruction(specialistInstruction("intent")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new intent agent fail: %v", buildErr)
	}
	globalIntentRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: intentAgent})

	riskAgent, buildErr := agent.NewAgentBuilder(cm).WithName("risk-agent").WithDescription("????????").WithInstruction(specialistInstruction("risk")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new risk agent fail: %v", buildErr)
	}
	globalRiskRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: riskAgent})

	departmentAgent, buildErr := agent.NewAgentBuilder(cm).WithName("department-agent").WithDescription("????????").WithInstruction(specialistInstruction("department")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new department agent fail: %v", buildErr)
	}
	globalDepartmentRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: departmentAgent})

	historyAgent, buildErr := agent.NewAgentBuilder(cm).WithName("history-agent").WithDescription("???????????").WithInstruction(specialistInstruction("history")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new history agent fail: %v", buildErr)
	}
	globalHistoryRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: historyAgent})

	medicationAgent, buildErr := agent.NewAgentBuilder(cm).WithName("medication-safety-agent").WithDescription("?????????").WithInstruction(specialistInstruction("medication")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new medication agent fail: %v", buildErr)
	}
	globalMedicationRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: medicationAgent})

	emergencyAgent, buildErr := agent.NewAgentBuilder(cm).WithName("emergency-agent").WithDescription("???????120??").WithInstruction(specialistInstruction("emergency")).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new emergency agent fail: %v", buildErr)
	}
	globalEmergencyRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: emergencyAgent})

	supervisorAgent, buildErr := agent.NewAgentBuilder(cm).WithName("supervisor-agent").WithDescription("??? Agent ?????????").WithInstruction(supervisorInstruction()).Build(ctx)
	if buildErr != nil {
		return fmt.Errorf("new supervisor agent fail: %v", buildErr)
	}
	globalSupervisorRunner = adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: supervisorAgent})

	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if _, ok := currentPatient(r); !ok {
		http.Redirect(w, r, "/patient/login", http.StatusSeeOther)
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
	patient, ok := currentPatient(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	w.Header().Set("X-Session-ID", sessionID)
	writer := sse.NewWriter(sessionID, w)
	if writer == nil {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	defer writer.Close()

	result := newTriageHarness().Run(r.Context(), triageHarnessInput{SessionID: sessionID, PatientPhone: patient.Phone, UserMessage: req.Message})
	if err := writeTextAsOpenAIStream(writer, result.Answer); err != nil {
		return
	}
	_ = writer.WriteDone()
	if result.Answer != "" {
		version := nextTriageExtractionVersion(sessionID)
		ragEvidence, _ := json.Marshal(result.KnowledgeHits)
		go extractAndSaveTriageRecord(sessionID, patient.Phone, req.Message, result.Answer, string(ragEvidence), version)
	}
}
