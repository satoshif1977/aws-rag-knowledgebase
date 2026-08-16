package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// ── 定数の値検証 ──────────────────────────────────────────────

func TestMaxDocumentBytes_Value(t *testing.T) {
	if maxDocumentBytes != 8000 {
		t.Errorf("maxDocumentBytes = %d, want 8000", maxDocumentBytes)
	}
}

func TestSystemPrompt_NotEmpty(t *testing.T) {
	if systemPrompt == "" {
		t.Error("systemPrompt should not be empty")
	}
}

func TestSystemPrompt_ContainsSocialPrompt(t *testing.T) {
	if !strings.Contains(systemPrompt, "社内規定") {
		t.Error("systemPrompt should contain '社内規定'")
	}
}

func TestSystemPrompt_ContainsDocumentNotFound(t *testing.T) {
	if !strings.Contains(systemPrompt, "記載がない") {
		t.Error("systemPrompt should describe fallback behavior for missing info")
	}
}

// ── getEnv 追加ケース ─────────────────────────────────────────

func TestGetEnv_NotExistKey_ReturnsDefault(t *testing.T) {
	got := getEnv("__DEFINITELY_NOT_SET_KEY_RAGKNL__", "mydefault")
	if got != "mydefault" {
		t.Errorf("got %q, want mydefault", got)
	}
}

func TestGetEnv_OverridesDefault(t *testing.T) {
	t.Setenv("RAGKNL_OVERRIDE_KEY", "override_value")
	got := getEnv("RAGKNL_OVERRIDE_KEY", "should_not_be_used")
	if got != "override_value" {
		t.Errorf("got %q, want override_value", got)
	}
}

// ── buildAPIResponse 追加ケース ───────────────────────────────

func TestBuildAPIResponse_NilBody(t *testing.T) {
	resp, err := buildAPIResponse(200, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body == "" {
		t.Error("Body should not be empty even for nil input")
	}
}

func TestBuildAPIResponse_404Status(t *testing.T) {
	resp, err := buildAPIResponse(404, map[string]string{"error": "not found"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
}

func TestBuildAPIResponse_BodyIsValidJSON(t *testing.T) {
	resp, _ := buildAPIResponse(200, map[string]any{"key": "value", "num": 42})
	if !json.Valid([]byte(resp.Body)) {
		t.Errorf("Body is not valid JSON: %s", resp.Body)
	}
}

func TestBuildAPIResponse_ArrayBody(t *testing.T) {
	resp, err := buildAPIResponse(200, []string{"item1", "item2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []string
	if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
		t.Fatalf("Body is not valid JSON array: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("array length = %d, want 2", len(got))
	}
}

func TestBuildAPIResponse_NestedStruct(t *testing.T) {
	nested := map[string]any{
		"answer": "テスト回答",
		"meta":   map[string]string{"source": "s3"},
	}
	resp, err := buildAPIResponse(200, nested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
		t.Fatalf("Body parse error: %v", err)
	}
	if got["answer"] != "テスト回答" {
		t.Errorf("answer = %v", got["answer"])
	}
}

// ── Request 構造体 追加ケース ─────────────────────────────────

func TestRequest_EmptyQuestion_ZeroValue(t *testing.T) {
	var req Request
	if req.Question != "" {
		t.Error("zero value Request.Question should be empty string")
	}
}

func TestRequest_LongQuestion(t *testing.T) {
	longQ := strings.Repeat("あ", 300)
	req := Request{Question: longQ}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Question != longQ {
		t.Error("long question should round-trip correctly")
	}
}

// ── Response 構造体 追加ケース ────────────────────────────────

func TestResponse_GeneralSource(t *testing.T) {
	r := Response{Answer: "一般知識の回答", Source: "general"}
	b, _ := json.Marshal(r)
	var got Response
	json.Unmarshal(b, &got)
	if got.Source != "general" {
		t.Errorf("Source = %q, want general", got.Source)
	}
}

func TestResponse_S3Source(t *testing.T) {
	r := Response{Answer: "ドキュメントベースの回答", Source: "s3_document"}
	b, _ := json.Marshal(r)
	var got Response
	json.Unmarshal(b, &got)
	if got.Source != "s3_document" {
		t.Errorf("Source = %q, want s3_document", got.Source)
	}
}

func TestResponse_EmptyFields(t *testing.T) {
	r := Response{}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var got Response
	json.Unmarshal(b, &got)
	if got.Answer != "" || got.Source != "" {
		t.Errorf("empty Response should round-trip: got %+v", got)
	}
}

// ── Message 構造体 追加ケース ─────────────────────────────────

func TestMessage_EmptyContent(t *testing.T) {
	msg := Message{Role: "user", Content: ""}
	b, _ := json.Marshal(msg)
	var got Message
	json.Unmarshal(b, &got)
	if got.Content != "" {
		t.Errorf("Content = %q, want empty", got.Content)
	}
}

func TestMessage_RoleIsPreserved(t *testing.T) {
	for _, role := range []string{"user", "assistant"} {
		msg := Message{Role: role, Content: "テスト"}
		b, _ := json.Marshal(msg)
		var got Message
		json.Unmarshal(b, &got)
		if got.Role != role {
			t.Errorf("Role = %q, want %q", got.Role, role)
		}
	}
}

// ── BedrockBody 追加ケース ────────────────────────────────────

func TestBedrockBody_MaxTokensPreserved(t *testing.T) {
	body := BedrockBody{MaxTokens: 1000}
	b, _ := json.Marshal(body)
	var got BedrockBody
	json.Unmarshal(b, &got)
	if got.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d, want 1000", got.MaxTokens)
	}
}

func TestBedrockBody_MultipleMessages(t *testing.T) {
	body := BedrockBody{
		Messages: []Message{
			{Role: "user", Content: "質問1"},
			{Role: "assistant", Content: "回答1"},
			{Role: "user", Content: "質問2"},
		},
	}
	b, _ := json.Marshal(body)
	var got BedrockBody
	json.Unmarshal(b, &got)
	if len(got.Messages) != 3 {
		t.Errorf("Messages len = %d, want 3", len(got.Messages))
	}
}

func TestBedrockBody_EmptyMessages(t *testing.T) {
	body := BedrockBody{Messages: []Message{}}
	b, _ := json.Marshal(body)
	var got BedrockBody
	json.Unmarshal(b, &got)
	if got.Messages == nil {
		t.Error("Messages should not be nil after round-trip")
	}
}

// ── BedrockResponse 追加ケース ────────────────────────────────

func TestBedrockResponse_TextPreserved(t *testing.T) {
	raw := `{"content": [{"text": "詳細な回答テキスト"}]}`
	var resp BedrockResponse
	json.Unmarshal([]byte(raw), &resp)
	if resp.Content[0].Text != "詳細な回答テキスト" {
		t.Errorf("Text = %q", resp.Content[0].Text)
	}
}

func TestBedrockResponse_NullContent(t *testing.T) {
	raw := `{"content": null}`
	var resp BedrockResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Content) != 0 {
		t.Errorf("null content should be empty slice, got len=%d", len(resp.Content))
	}
}

// ── Handler 追加ケース ────────────────────────────────────────

func TestHandler_ValidQuestion_NotReturn400(t *testing.T) {
	// 有効な質問はバリデーション通過（400 にはならない・AWS エラーは 500）
	body, _ := json.Marshal(Request{Question: "社内規程について教えてください"})
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: string(body)})
	if resp.StatusCode == 400 {
		t.Error("有効な質問は 400 を返すべきでない")
	}
}

func TestHandler_LongQuestion_NotReturn400(t *testing.T) {
	longQ := strings.Repeat("あ", 200)
	body, _ := json.Marshal(Request{Question: longQ})
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: string(body)})
	if resp.StatusCode == 400 {
		t.Error("長い質問は 400 を返すべきでない")
	}
}

func TestHandler_ExtraJSONFields_NotReturn400(t *testing.T) {
	// 未知フィールドは無視される → バリデーション通過
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"question":"テスト質問","unknown_field":"ignored"}`,
	})
	if resp.StatusCode == 400 {
		t.Error("未知フィールドがあっても 400 を返すべきでない")
	}
}

func TestHandler_400_BodyIsNotEmpty(t *testing.T) {
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: "bad"})
	if resp.Body == "" {
		t.Error("400 response body should not be empty")
	}
}

// ── Benchmark ─────────────────────────────────────────────────

func BenchmarkBuildAPIResponse(b *testing.B) {
	body := Response{Answer: "ベンチマーク回答", Source: "s3_document"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildAPIResponse(200, body) //nolint:errcheck
	}
}

func BenchmarkGetEnv(b *testing.B) {
	b.Setenv("BENCH_KEY_RAGKNL", "bench_value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getEnv("BENCH_KEY_RAGKNL", "fallback")
	}
}

func BenchmarkHandlerValidation(b *testing.B) {
	event := events.APIGatewayProxyRequest{
		Body: `{"question":"ベンチマーク用質問テキスト"}`,
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Handler(ctx, event) //nolint:errcheck
	}
}

// ── systemPrompt 詳細検証 ─────────────────────────────────────

func TestSystemPrompt_ContainsAssistantKeyword(t *testing.T) {
	if !strings.Contains(systemPrompt, "専門アシスタント") {
		t.Error("systemPrompt should contain '専門アシスタント'")
	}
}

func TestSystemPrompt_ContainsDepartmentKeyword(t *testing.T) {
	if !strings.Contains(systemPrompt, "担当部署") {
		t.Error("systemPrompt should contain '担当部署'")
	}
}

func TestSystemPrompt_LengthAtLeast100(t *testing.T) {
	if len([]rune(systemPrompt)) < 100 {
		t.Errorf("systemPrompt too short: %d runes", len([]rune(systemPrompt)))
	}
}

func TestMaxDocumentBytes_IsPositive(t *testing.T) {
	if maxDocumentBytes <= 0 {
		t.Errorf("maxDocumentBytes should be positive, got %d", maxDocumentBytes)
	}
}

// ── Handler 追加バリデーション ────────────────────────────────

func TestHandler_CRLFOnlyQuestion_Returns400(t *testing.T) {
	body, _ := json.Marshal(Request{Question: "\r\n"})
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: string(body)})
	if resp.StatusCode != 400 {
		t.Errorf("CRLF のみ question は 400 を返すべき: got %d", resp.StatusCode)
	}
}

func TestHandler_ArrayBody_Returns400(t *testing.T) {
	// JSON 配列はオブジェクトへの unmarshal に失敗 → 400
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `[{"question":"テスト"}]`,
	})
	if resp.StatusCode != 400 {
		t.Errorf("配列 body は 400 を返すべき: got %d", resp.StatusCode)
	}
}

func TestHandler_QuestionAsArray_Returns400(t *testing.T) {
	// question フィールドが配列型 → string への unmarshal 失敗 → 400
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"question": ["item1", "item2"]}`,
	})
	if resp.StatusCode != 400 {
		t.Errorf("question が配列の場合は 400 を返すべき: got %d", resp.StatusCode)
	}
}

func TestHandler_BooleanQuestion_Returns400(t *testing.T) {
	// question フィールドが boolean → string への unmarshal 失敗 → 400
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"question": true}`,
	})
	if resp.StatusCode != 400 {
		t.Errorf("question が boolean の場合は 400 を返すべき: got %d", resp.StatusCode)
	}
}

// ── buildAPIResponse 追加ケース ───────────────────────────────

func TestBuildAPIResponse_IntBody(t *testing.T) {
	resp, err := buildAPIResponse(200, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body != "42" {
		t.Errorf("Body = %q, want %q", resp.Body, "42")
	}
}

func TestBuildAPIResponse_422Status(t *testing.T) {
	resp, err := buildAPIResponse(422, map[string]string{"error": "validation failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 422 {
		t.Errorf("StatusCode = %d, want 422", resp.StatusCode)
	}
}

func TestBuildAPIResponse_AllStatusesHaveCORSHeader(t *testing.T) {
	// 200/400/404/500 すべてで CORS ヘッダーが付くこと
	for _, code := range []int{200, 400, 404, 500} {
		resp, err := buildAPIResponse(code, map[string]string{})
		if err != nil {
			t.Fatalf("status=%d: unexpected error: %v", code, err)
		}
		if resp.Headers["Access-Control-Allow-Origin"] != "*" {
			t.Errorf("status %d: CORS header missing or wrong: %q", code, resp.Headers["Access-Control-Allow-Origin"])
		}
	}
}

// ── BedrockBody 追加ケース ────────────────────────────────────

func TestBedrockBody_AnthropicVersion(t *testing.T) {
	body := BedrockBody{AnthropicVersion: "bedrock-2023-05-31"}
	b, _ := json.Marshal(body)
	var got BedrockBody
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.AnthropicVersion != "bedrock-2023-05-31" {
		t.Errorf("AnthropicVersion = %q, want bedrock-2023-05-31", got.AnthropicVersion)
	}
}

func TestBedrockBody_SystemPreserved(t *testing.T) {
	body := BedrockBody{System: systemPrompt, MaxTokens: 1000}
	b, _ := json.Marshal(body)
	var got BedrockBody
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.System != systemPrompt {
		t.Errorf("System field not preserved: got len=%d, want len=%d", len(got.System), len(systemPrompt))
	}
}

// ── getEnv スペース文字 ───────────────────────────────────────

func TestGetEnv_SpaceValueNotFallback(t *testing.T) {
	// スペース文字は空文字でないため fallback しない（getEnv は v != "" で判定）
	t.Setenv("TEST_SPACE_KEY_RAG", " ")
	got := getEnv("TEST_SPACE_KEY_RAG", "fallback")
	if got != " " {
		t.Errorf("スペース値は fallback しない: got %q, want \" \"", got)
	}
}

// ── Request / Response 追加ケース ────────────────────────────

func TestRequest_UnicodeQuestion(t *testing.T) {
	// Unicode 文字（絵文字・記号）が round-trip で保持される
	q := "有給休暇の申請方法は？🎉"
	req := Request{Question: q}
	b, _ := json.Marshal(req)
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Question != q {
		t.Errorf("Question = %q, want %q", got.Question, q)
	}
}

func TestResponse_AnswerAndSourcePreserved(t *testing.T) {
	r := Response{Answer: "詳細な回答テキストです", Source: "s3_document"}
	b, _ := json.Marshal(r)
	var got Response
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Answer != r.Answer {
		t.Errorf("Answer = %q, want %q", got.Answer, r.Answer)
	}
	if got.Source != r.Source {
		t.Errorf("Source = %q, want %q", got.Source, r.Source)
	}
}

func TestHandler_TabOnlyQuestion_Returns400(t *testing.T) {
	// タブ文字のみの quality は TrimSpace 後に空文字 → 400
	body, _ := json.Marshal(Request{Question: "\t\t\t"})
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: string(body)})
	if resp.StatusCode != 400 {
		t.Errorf("タブのみ question は 400 を返すべき: got %d", resp.StatusCode)
	}
}
