package main

import "testing"

func TestNeedsAnswerReview(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "ordinary symptom", message: "\u6211\u6709\u70b9\u5e72\u54b3\uff0c\u6ca1\u6709\u53d1\u70e7", want: false},
		{name: "pregnancy medication", message: "\u6211\u662f\u5b55\u5987\uff0c\u5e72\u54b3\uff0c\u53ef\u4ee5\u5403\u4ec0\u4e48\u836f", want: true},
		{name: "emergency", message: "\u6211\u80f8\u75db\uff0c\u800c\u4e14\u5598\u4e0d\u4e0a\u6c14", want: true},
		{name: "child", message: "\u513f\u7ae5\u53d1\u70e7\u5e94\u8be5\u600e\u4e48\u529e", want: true},
		{name: "allergy", message: "\u6211\u6709\u9752\u9709\u7d20\u8fc7\u654f\u53f2", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := needsAnswerReview(test.message); got != test.want {
				t.Fatalf("needsAnswerReview(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}

func TestNormalizeExtractorJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: `{"riskLevel":"P3"}`, want: `{"riskLevel":"P3"}`},
		{name: "markdown fence", input: "```json\n{\"riskLevel\":\"P1\"}\n```", want: `{"riskLevel":"P1"}`},
		{name: "surrounding text", input: "result: {\"department\":\"\u6025\u8bca\u79d1\"} done", want: "{\"department\":\"\u6025\u8bca\u79d1\"}"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeExtractorJSON(test.input); got != test.want {
				t.Fatalf("normalizeExtractorJSON(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestTriageExtractionVersion(t *testing.T) {
	triageExtractionMu.Lock()
	triageExtractionVersions = make(map[string]uint64)
	triageExtractionMu.Unlock()

	first := nextTriageExtractionVersion("session-a")
	second := nextTriageExtractionVersion("session-a")
	other := nextTriageExtractionVersion("session-b")

	if first != 1 || second != 2 || other != 1 {
		t.Fatalf("unexpected versions: first=%d second=%d other=%d", first, second, other)
	}
	if isLatestTriageExtraction("session-a", first) {
		t.Fatal("older extraction must not be latest")
	}
	if !isLatestTriageExtraction("session-a", second) {
		t.Fatal("newest extraction must be latest")
	}
}
