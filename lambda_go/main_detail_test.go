package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// ── getDocumentFromS3 ローカルロジック ───────────────────────────────

func TestGetDocumentFromS3_EmptyBucket(t *testing.T) {
	// bucketName が空の場合はAWS呼び出しをスキップして空文字を返す
	orig := bucketName
	bucketName = ""
	defer func() { bucketName = orig }()

	text, err := getDocumentFromS3(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string for empty bucket, got %q", text)
	}
}

// ── Request 構造体 JSON ──────────────────────────────────────────────

func TestRequestJSON_Marshal(t *testing.T) {
	req := Request{Question: "有給休暇の申請方法は？"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !json.Valid(b) {
		t.Error("marshaled JSON is not valid")
	}
}

func TestRequestJSON_Unmarshal(t *testing.T) {
	raw := `{"question": "就業規則について教えてください"}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Question != "就業規則について教えてください" {
		t.Errorf("got %q", req.Question)
	}
}

func TestRequestJSON_RoundTrip(t *testing.T) {
	orig := Request{Question: "テスト質問"}
	b, _ := json.Marshal(orig)
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Question != orig.Question {
		t.Errorf("got %q, want %q", got.Question, orig.Question)
	}
}

// ── Message 構造体 JSON ──────────────────────────────────────────────

func TestMessageJSON_RoundTrip(t *testing.T) {
	msg := Message{Role: "user", Content: "こんにちは"}
	b, _ := json.Marshal(msg)
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Role != "user" || got.Content != "こんにちは" {
		t.Errorf("got role=%q content=%q", got.Role, got.Content)
	}
}

func TestMessageJSON_AssistantRole(t *testing.T) {
	msg := Message{Role: "assistant", Content: "回答です"}
	b, _ := json.Marshal(msg)
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Role != "assistant" {
		t.Errorf("got role %q, want assistant", got.Role)
	}
}

// ── BedrockResponse 構造体 JSON ──────────────────────────────────────

func TestBedrockResponseJSON_Parse(t *testing.T) {
	raw := `{"content": [{"text": "これが回答です"}]}`
	var resp BedrockResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("content length: got %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Text != "これが回答です" {
		t.Errorf("got %q", resp.Content[0].Text)
	}
}

func TestBedrockResponseJSON_MultipleContent(t *testing.T) {
	raw := `{"content": [{"text": "first"}, {"text": "second"}]}`
	var resp BedrockResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("content length: got %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Text != "first" {
		t.Errorf("first content: got %q", resp.Content[0].Text)
	}
}

func TestBedrockResponseJSON_EmptyContent(t *testing.T) {
	raw := `{"content": []}`
	var resp BedrockResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(resp.Content) != 0 {
		t.Errorf("expected empty content, got %d items", len(resp.Content))
	}
}

// ── Handler 400 ヘッダー検証 ──────────────────────────────────────────

func TestHandler_400_HasContentTypeHeader(t *testing.T) {
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: "bad"})
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if got := resp.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", got)
	}
}

func TestHandler_400_HasCORSHeader(t *testing.T) {
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: "{}"})
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if got := resp.Headers["Access-Control-Allow-Origin"]; got != "*" {
		t.Errorf("CORS: got %q, want *", got)
	}
}

func TestHandler_NullQuestion(t *testing.T) {
	// JSON null は string のゼロ値（空文字）に変換される → TrimSpace("") == "" → 400
	resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"question": null}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

func TestHandler_NumberQuestion(t *testing.T) {
	// 数値は string にアンマーシャルできない → JSON エラー → 400
	resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"question": 123}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

func TestHandler_NewlineOnlyQuestion(t *testing.T) {
	// 改行のみの質問は TrimSpace で空文字になる → 400
	body, _ := json.Marshal(Request{Question: "\n\n\n"})
	resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: string(body),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

// ── buildAPIResponse 追加ケース ────────────────────────────────────────

func TestBuildAPIResponse_EmptyResponseStruct(t *testing.T) {
	resp, err := buildAPIResponse(200, Response{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got Response
	if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if got.Answer != "" || got.Source != "" {
		t.Errorf("expected empty Response fields, got %+v", got)
	}
}

func TestBuildAPIResponse_HeaderMapNotNil(t *testing.T) {
	resp, _ := buildAPIResponse(200, map[string]string{})
	if resp.Headers == nil {
		t.Error("Headers map should not be nil")
	}
}
