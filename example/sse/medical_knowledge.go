package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type MedicalKnowledgeDocument struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Code         string     `gorm:"size:64;uniqueIndex" json:"code"`
	Title        string     `gorm:"size:255" json:"title"`
	Content      string     `gorm:"type:text" json:"content"`
	Keywords     string     `gorm:"type:text" json:"keywords"`
	SourceName   string     `gorm:"size:128" json:"sourceName"`
	SourceURL    string     `gorm:"size:512" json:"sourceUrl"`
	Enabled      bool       `gorm:"default:true;index" json:"enabled"`
	ReviewStatus string     `gorm:"size:24;default:approved;index" json:"reviewStatus"`
	Version      int        `gorm:"default:1" json:"version"`
	ReviewedBy   string     `gorm:"size:128" json:"reviewedBy"`
	ReviewedAt   *time.Time `json:"reviewedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type medicalKnowledgeHit struct {
	Code            string     `json:"code"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	SourceName      string     `json:"sourceName"`
	SourceURL       string     `json:"sourceUrl"`
	Version         int        `json:"version"`
	Score           int        `json:"score"`
	ReviewedBy      string     `json:"reviewedBy,omitempty"`
	ReviewedAt      *time.Time `json:"reviewedAt,omitempty"`
	RetrievedAt     time.Time  `json:"retrievedAt"`
	MatchReasons    []string   `json:"matchReasons,omitempty"`
	ExcludedReasons []string   `json:"excludedReasons,omitempty"`
}

type medicalKnowledgeExclusion struct {
	Code    string   `json:"code"`
	Title   string   `json:"title"`
	Reasons []string `json:"reasons"`
}

type medicalKnowledgeRetrievalAudit struct {
	Hits           []medicalKnowledgeHit       `json:"hits"`
	Excluded       []medicalKnowledgeExclusion `json:"excluded,omitempty"`
	Evaluated      int                         `json:"evaluated"`
	CurrentInput   string                      `json:"currentInput,omitempty"`
	ContextSummary string                      `json:"contextSummary,omitempty"`
}

func seedMedicalKnowledge() error {
	seedReviewedAt := time.Now().UTC()
	documents := []MedicalKnowledgeDocument{
		{Code: "EMERGENCY-CHEST-PAIN", Title: "胸痛与呼吸困难急诊分诊", Content: "新发、剧烈、持续或逐渐加重的胸痛，伴明显呼吸困难、晕厥、口唇发紫或大汗时，应立即进行急诊评估。不要因等待线上回复而延误拨打 120 或前往急诊。", Keywords: "chest pain,pressure,chest tightness,shortness of breath,cannot breathe,breathing difficulty,fainting,blue lips,120,emergency,胸痛,胸闷,胸口压迫感,呼吸困难,喘不上气,晕厥,昏倒,嘴唇发紫,急诊", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/chestpain.html", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "EMERGENCY-STROKE", Title: "疑似脑卒中预警信号", Content: "突然出现口角歪斜、单侧肢体无力或麻木、言语不清、意识混乱、视物异常、失去平衡或突发剧烈头痛，属于脑卒中急诊预警信号。记录症状开始时间并立即呼叫急救。", Keywords: "face droop,arm weakness,numbness,speech difficulty,slurred speech,confusion,vision trouble,severe headache,stroke,120,emergency,口角歪斜,单侧无力,肢体麻木,说话不清,言语含糊,意识混乱,视物不清,突发剧烈头痛,中风,脑卒中,急诊", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/stroke.html", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "EMERGENCY-BLEEDING", Title: "严重出血与意识丧失分诊", Content: "无法控制的出血、呕血、黑便或便血，同时伴明显乏力、意识丧失、反复抽搐或严重外伤时，应按急症处理。症状严重时应呼叫 120，避免自行驾车。", Keywords: "heavy bleeding,uncontrolled bleeding,vomiting blood,black stool,bloody stool,unconscious,seizure,convulsion,trauma,120,emergency,大出血,止不住血,呕血,黑便,便血,昏迷,失去意识,抽搐,惊厥,外伤,急诊", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/ency/article/000045.htm", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "FEVER-TRIAGE", Title: "发热分诊与重点人群", Content: "评估发热时需要结合持续时间、最高体温、呼吸情况、饮水与排尿、皮疹、精神状态、年龄、妊娠、慢性病及免疫状态。婴幼儿、高龄老人、孕妇和严重慢病患者在持续或加重发热时应更早线下就诊。", Keywords: "fever,high fever,temperature,child,infant,pregnant,elderly,immunocompromised,rash,dehydration,发热,发烧,高热,体温,儿童,婴儿,孕妇,怀孕,老人,高龄,免疫低下,皮疹,脱水", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/fever.html", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "RESPIRATORY-TRIAGE", Title: "呼吸道症状分诊", Content: "咳嗽、咽痛、鼻部症状或无急诊信号的轻度稳定发热，需要询问病程、体温、呼吸状态、痰液、接触史、慢性肺病和用药史，再推荐合适门诊。若呼吸困难加重，应优先转入急诊评估。", Keywords: "cough,sore throat,runny nose,phlegm,sputum,respiratory,asthma,breathing difficulty,咳嗽,干咳,喉咙痛,流鼻涕,痰,呼吸道,哮喘,呼吸困难", SourceName: "世界卫生组织", SourceURL: "https://www.who.int/health-topics/respiratory-tract-diseases", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "ABDOMINAL-TRIAGE", Title: "腹部症状分诊", Content: "腹痛需要询问疼痛部位、起病时间、变化趋势、发热、呕吐、排便变化、妊娠可能、手术史和补液情况。持续剧痛、腹部僵硬、晕厥、呕血便血或妊娠相关腹痛，应尽快线下急诊评估。", Keywords: "abdominal pain,stomach pain,belly pain,vomiting,diarrhea,constipation,blood stool,pregnancy,appendix,腹痛,肚子痛,胃痛,呕吐,腹泻,便秘,便血,怀孕,阑尾", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/abdominalpain.html", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "MEDICATION-SAFETY", Title: "用药与过敏安全边界", Content: "咨询用药时需确认药名、剂量、用途、过敏史、妊娠情况、年龄、肝肾疾病及其他用药。未经医生或药师复核，不提供个体化剂量调整或擅自停药建议。用药后呼吸困难、面部或咽喉肿胀、广泛荨麻疹属于急诊信号。", Keywords: "medicine,drug,dose,how to take,allergy,hives,face swelling,throat swelling,pregnancy,kidney,liver,药,药物,剂量,怎么吃,过敏,荨麻疹,脸肿,喉咙肿,怀孕,肾病,肝病", SourceName: "MedlinePlus", SourceURL: "https://medlineplus.gov/drugsafety.html", Enabled: true, ReviewStatus: "approved", Version: 1},
		{Code: "DEIDENTIFIED-CASE-RESPIRATORY", Title: "脱敏导诊案例：稳定呼吸道症状", Content: "案例：成年患者咳嗽、咽痛两天，无胸痛、严重呼吸困难、意识混乱或晕厥。导诊重点是继续询问体温、呼吸、痰液、接触史、慢性肺病和症状变化，在不作疾病诊断的前提下给出保守门诊建议与病情加重提示。", Keywords: "case,cough,sore throat,adult,stable,respiratory,outpatient,案例,咳嗽,喉咙痛,成人,稳定,呼吸道,门诊", SourceName: "项目脱敏教学案例", SourceURL: "https://www.who.int/health-topics/respiratory-tract-diseases", Enabled: true, ReviewStatus: "approved", Version: 1},
	}
	for index := range documents {
		documents[index].ReviewedBy = "系统预置审核"
		reviewedAt := seedReviewedAt
		documents[index].ReviewedAt = &reviewedAt
	}
	for _, document := range documents {
		var existing MedicalKnowledgeDocument
		result := globalDB.Where("code = ?", document.Code).First(&existing)
		if result.Error == nil {
			migrated, changed := applySeededKnowledgeMigration(existing, document, seedReviewedAt)
			if changed {
				if err := globalDB.Save(&migrated).Error; err != nil {
					return err
				}
			}
			continue
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		if err := globalDB.Create(&document).Error; err != nil {
			return err
		}
	}
	return nil
}

func shouldMigrateSeededKnowledge(document MedicalKnowledgeDocument) bool {
	return document.Version <= 1
}

func applySeededKnowledgeMigration(existing, seeded MedicalKnowledgeDocument, reviewTime time.Time) (MedicalKnowledgeDocument, bool) {
	if !shouldMigrateSeededKnowledge(existing) {
		return existing, false
	}
	changed := false
	setString := func(target *string, value string) {
		if *target != value {
			*target = value
			changed = true
		}
	}
	setString(&existing.Title, seeded.Title)
	setString(&existing.Content, seeded.Content)
	setString(&existing.Keywords, seeded.Keywords)
	setString(&existing.SourceName, seeded.SourceName)
	setString(&existing.SourceURL, seeded.SourceURL)
	if existing.ReviewStatus == "" {
		existing.ReviewStatus = "approved"
		changed = true
	}
	if existing.Version < 1 {
		existing.Version = 1
		changed = true
	}
	if existing.ReviewStatus == "approved" {
		if strings.TrimSpace(existing.ReviewedBy) == "" {
			existing.ReviewedBy = "系统预置审核"
			changed = true
		}
		if existing.ReviewedAt == nil {
			reviewedAt := reviewTime
			if !existing.CreatedAt.IsZero() {
				reviewedAt = existing.CreatedAt
			}
			existing.ReviewedAt = &reviewedAt
			changed = true
		}
	}
	return existing, changed
}

func retrieveMedicalKnowledge(query string, limit int) []medicalKnowledgeHit {
	return retrieveMedicalKnowledgeWithAudit(query, limit).Hits
}

func retrieveMedicalKnowledgeWithAudit(query string, limit int) medicalKnowledgeRetrievalAudit {
	audit := medicalKnowledgeRetrievalAudit{}
	if globalDB == nil || limit <= 0 {
		return audit
	}
	normalized := normalizeKnowledgeText(query)
	if normalized == "" {
		return audit
	}
	var documents []MedicalKnowledgeDocument
	if globalDB.Where("enabled = ? AND review_status = ?", true, "approved").Find(&documents).Error != nil {
		return audit
	}
	audit.Evaluated = len(documents)
	candidates := make([]medicalKnowledgeHit, 0, len(documents))
	for _, document := range documents {
		score, reasons, excludedReasons := knowledgeMatchEvaluation(normalized, document)
		if score == 0 {
			if len(excludedReasons) > 0 {
				audit.Excluded = append(audit.Excluded, medicalKnowledgeExclusion{Code: document.Code, Title: document.Title, Reasons: excludedReasons})
			}
			continue
		}
		candidates = append(candidates, medicalKnowledgeHit{Code: document.Code, Title: document.Title, Content: document.Content, SourceName: document.SourceName, SourceURL: document.SourceURL, Version: document.Version, Score: score, ReviewedBy: document.ReviewedBy, ReviewedAt: document.ReviewedAt, RetrievedAt: time.Now().UTC(), MatchReasons: reasons, ExcludedReasons: excludedReasons})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Code < candidates[j].Code
		}
		return candidates[i].Score > candidates[j].Score
	})
	familyCounts := map[string]int{}
	for _, hit := range candidates {
		family := knowledgeFamily(hit.Code)
		if familyCounts[family] >= 2 {
			audit.Excluded = append(audit.Excluded, medicalKnowledgeExclusion{Code: hit.Code, Title: hit.Title, Reasons: []string{"同主题知识已达到展示上限"}})
			continue
		}
		audit.Hits = append(audit.Hits, hit)
		familyCounts[family]++
		if len(audit.Hits) == limit {
			break
		}
	}
	return audit
}

func normalizeKnowledgeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("，", " ", "。", " ", "、", " ", "；", " ", "：", " ", "？", " ", "！", " ", "（", " ", "）", " ", ",", " ", ".", " ", ";", " ", ":", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func splitKnowledgeKeywords(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})
	keywords := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		keyword := normalizeKnowledgeText(part)
		if len([]rune(keyword)) < 2 || seen[keyword] {
			continue
		}
		seen[keyword] = true
		keywords = append(keywords, keyword)
	}
	return keywords
}

func keywordNegated(query, keyword string) bool {
	queryRunes, keywordRunes := []rune(query), []rune(keyword)
	if len(keywordRunes) == 0 || len(queryRunes) < len(keywordRunes) {
		return false
	}
	found := false
	negations := []string{"没有", "否认", "不是", "未出现", "未见", "不伴", "并无", "无明显", "无", "没", "未"}
	for index := 0; index <= len(queryRunes)-len(keywordRunes); index++ {
		if string(queryRunes[index:index+len(keywordRunes)]) != keyword {
			continue
		}
		found = true
		start := index - 8
		if start < 0 {
			start = 0
		}
		prefix := strings.ReplaceAll(string(queryRunes[start:index]), " ", "")
		negated := false
		correctionTurnsPositive := strings.Contains(prefix, "不是") && strings.HasSuffix(prefix, "是") && !strings.HasSuffix(prefix, "不是")
		if correctionTurnsPositive {
			return false
		}
		for _, marker := range negations {
			if strings.Contains(prefix, marker) {
				negated = true
				break
			}
		}
		if !negated {
			return false
		}
	}
	return found
}

func knowledgeMatchEvaluation(query string, document MedicalKnowledgeDocument) (int, []string, []string) {
	score := 0
	reasons := make([]string, 0, 6)
	excluded := make([]string, 0, 3)
	generic := map[string]bool{"120": true, "emergency": true, "急诊": true, "门诊": true}
	for _, keyword := range splitKnowledgeKeywords(document.Keywords) {
		if !strings.Contains(query, keyword) {
			continue
		}
		if keywordNegated(query, keyword) {
			excluded = append(excluded, fmt.Sprintf("否定症状“%s”不计分", keyword))
			continue
		}
		points := 4
		if generic[keyword] {
			points = 1
		}
		score += points
		reasons = append(reasons, fmt.Sprintf("关键词“%s” +%d", keyword, points))
	}
	if strings.HasPrefix(document.Code, "EMERGENCY-") && score <= 1 {
		if len(excluded) > 0 {
			excluded = append(excluded, "急诊知识缺少正向危险信号，已排除")
		}
		return 0, nil, excluded
	}
	return score, reasons, excluded
}

func knowledgeScore(query string, document MedicalKnowledgeDocument) int {
	score, _, _ := knowledgeMatchEvaluation(normalizeKnowledgeText(query), document)
	return score
}

func knowledgeFamily(code string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	for _, family := range []string{"RESPIRATORY", "CHEST", "STROKE", "BLEEDING", "FEVER", "ABDOMINAL", "MEDICATION"} {
		if strings.Contains(upper, family) {
			return family
		}
	}
	return upper
}
func formatMedicalKnowledgeContext(hits []medicalKnowledgeHit) string {
	if len(hits) == 0 {
		return ""
	}
	lines := []string{"<medical_knowledge_references>", "The following references are triage-only evidence. Use them conservatively; do not diagnose, prescribe, or claim certainty."}
	for _, hit := range hits {
		lines = append(lines, "["+hit.Code+"] "+hit.Title+"\n"+hit.Content+"\nSource: "+hit.SourceName+" | "+hit.SourceURL)
	}
	lines = append(lines, "</medical_knowledge_references>")
	return strings.Join(lines, "\n")
}
