package main

import (
	"context"
	"encoding/json"
	"strings"

	agmsg "github.com/CoolBanHub/aggo/internal/agentic"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type reviewResult struct {
	Approved bool   `json:"approved"`
	Answer   string `json:"answer"`
	Reason   string `json:"reason"`
}

func reviewDescription() string {
	return "\u5bfc\u8bca\u56de\u7b54\u5b89\u5168\u4e0e\u8d28\u91cf\u5ba1\u6838\u5458"
}

func reviewInstruction() string {
	return strings.Join([]string{
		"\u4f60\u662f\u72ec\u7acb\u7684\u5bfc\u8bca\u56de\u7b54 Review Agent\u3002\u4f60\u53ea\u8d1f\u8d23\u5ba1\u6838\u4e0e\u5fc5\u8981\u4fee\u6b63\uff0c\u4e0d\u4e0e\u60a3\u8005\u5bf9\u8bdd\u3002",
		"\u53ea\u8f93\u51fa JSON\uff1a{\"approved\":true|false,\"answer\":\"\u6700\u7ec8\u53ef\u53d1\u9001\u56de\u7b54\",\"reason\":\"\u7b80\u77ed\u5ba1\u6838\u7406\u7531\"}\u3002\u4e0d\u8981 Markdown \u6216\u5176\u4ed6\u6587\u5b57\u3002",
		"\u5ba1\u6838\u9879\uff1a\u662f\u5426\u627f\u63a5\u4e0a\u4e0b\u6587\u5e76\u56de\u7b54\u5f53\u524d\u95ee\u9898\uff1b\u662f\u5426\u9057\u6f0f\u6025\u75c7\u4fe1\u53f7\uff1b\u662f\u5426\u8fc7\u5ea6\u786e\u8bca\u3001\u4e71\u5f00\u836f\u6216\u4e71\u7ed9\u5242\u91cf\uff1b\u662f\u5426\u5bf9\u513f\u7ae5\u3001\u5b55\u5987\u3001\u8001\u4eba\u548c\u6162\u6027\u75c5\u4eba\u7fa4\u8db3\u591f\u8c28\u614e\uff1b\u662f\u5426\u8fc7\u957f\u6216\u91cd\u590d\u3002",
		"\u5982\u679c\u4e0d\u5408\u683c\uff0capproved=false \u5e76\u5728 answer \u4e2d\u7ed9\u51fa\u4e00\u7248\u53ef\u76f4\u63a5\u53d1\u9001\u7ed9\u60a3\u8005\u7684\u7b80\u6d01\u4fee\u6b63\u7a3f\u3002\u4e0d\u5f97\u65b0\u589e\u7528\u6237\u672a\u63d0\u4f9b\u7684\u75c7\u72b6\u6216\u68c0\u67e5\u7ed3\u679c\u3002",
	}, "\n")
}

func needsAnswerReview(message string) bool {
	return hasPositiveDispatchKeyword(message,
		"药", "抗生素", "剂量", "怎么吃", "胸痛", "胸闷", "呼吸困难", "喘不上气", "意识", "昏迷", "抽搐", "大出血", "单侧无力", "咳血",
		"孕妇", "怀孕", "孕期", "儿童", "宝宝", "婴儿", "老人", "高龄", "慢性病", "过敏",
	)
}

func collectAgentReply(ctx context.Context, runner *adk.TypedRunner[*schema.AgenticMessage], messages []*schema.AgenticMessage, options ...adk.AgentRunOption) (string, error) {
	iter := runner.Run(ctx, messages, options...)
	var output strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return "", event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err == nil && msg != nil {
			output.WriteString(agmsg.Text(msg))
		}
	}
	return strings.TrimSpace(output.String()), nil
}

func reviewAnswer(ctx context.Context, userMessage, draft string) (string, reviewResult, error) {
	return reviewAnswerWithKnowledge(ctx, userMessage, "", draft)
}

func reviewAnswerWithKnowledge(ctx context.Context, userMessage, knowledgeContext, draft string) (string, reviewResult, error) {
	result := reviewResult{Approved: true, Answer: strings.TrimSpace(draft)}
	if globalReviewRunner == nil {
		return result.Answer, result, nil
	}
	promptParts := []string{"<user_message>", userMessage, "</user_message>"}
	if strings.TrimSpace(knowledgeContext) != "" {
		promptParts = append(promptParts, knowledgeContext)
	}
	promptParts = append(promptParts, "<draft_answer>", draft, "</draft_answer>", "\u8bf7\u6839\u636e\u5ba1\u6838\u89c4\u5219\u8f93\u51fa JSON\u3002")
	prompt := strings.Join(promptParts, "\n")
	reviewText, err := collectAgentReply(ctx, globalReviewRunner, []*schema.AgenticMessage{schema.UserAgenticMessage(prompt)})
	if err != nil {
		return result.Answer, result, err
	}
	if err := json.Unmarshal([]byte(normalizeExtractorJSON(reviewText)), &result); err != nil {
		return strings.TrimSpace(draft), reviewResult{Approved: true, Answer: strings.TrimSpace(draft), Reason: "review_parse_failed"}, err
	}
	result.Answer = strings.TrimSpace(result.Answer)
	if result.Answer == "" {
		result.Answer = strings.TrimSpace(draft)
	}
	if result.Approved {
		return strings.TrimSpace(draft), result, nil
	}
	return result.Answer, result, nil
}
