package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// ── getEnv ──────────────────────────────────────────────────

func TestGetEnv_WithValue(t *testing.T) {
	t.Setenv("TEST_KEY", "hello")
	if got := getEnv("TEST_KEY", "default"); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestGetEnv_Fallback(t *testing.T) {
	os.Unsetenv("TEST_KEY_MISSING")
	if got := getEnv("TEST_KEY_MISSING", "fallback"); got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

// ── buildAPIResponse ────────────────────────────────────────

func TestBuildAPIResponse_StatusCode(t *testing.T) {
	resp, err := buildAPIResponse(200, map[string]string{"answer": "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("got status %d, want 200", resp.StatusCode)
	}
}

func TestBuildAPIResponse_CORSHeader(t *testing.T) {
	resp, _ := buildAPIResponse(200, map[string]string{})
	if got := resp.Headers["Access-Control-Allow-Origin"]; got != "*" {
		t.Errorf("CORS header got %q, want %q", got, "*")
	}
}

func TestBuildAPIResponse_ContentType(t *testing.T) {
	resp, _ := buildAPIResponse(400, map[string]string{"error": "bad request"})
	if got := resp.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type got %q, want application/json", got)
	}
}

func TestBuildAPIResponse_BodyJSON(t *testing.T) {
	resp, _ := buildAPIResponse(200, Response{Answer: "テスト回答", Source: "s3_document"})
	var r Response
	if err := json.Unmarshal([]byte(resp.Body), &r); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if r.Answer != "テスト回答" {
		t.Errorf("got answer %q, want %q", r.Answer, "テスト回答")
	}
	if r.Source != "s3_document" {
		t.Errorf("got source %q, want %q", r.Source, "s3_document")
	}
}

// ── Handler（400 系：AWS 呼び出し前に返るためモック不要） ──

func TestHandler_InvalidJSON(t *testing.T) {
	resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: "not-json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

func TestHandler_EmptyQuestion(t *testing.T) {
	body, _ := json.Marshal(Request{Question: ""})
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

func TestHandler_WhitespaceOnlyQuestion(t *testing.T) {
	body, _ := json.Marshal(Request{Question: "   "})
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

func TestHandler_EmptyBody(t *testing.T) {
	resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{
		Body: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

// ── getEnv 追加ケース ────────────────────────────────────────

func TestGetEnv_EmptyStringFallsBack(t *testing.T) {
	// 空文字がセットされている場合もフォールバックが使われること
	t.Setenv("TEST_EMPTY_KEY", "")
	if got := getEnv("TEST_EMPTY_KEY", "default"); got != "default" {
		t.Errorf("got %q, want %q", got, "default")
	}
}

func TestGetEnv_TableDriven(t *testing.T) {
	tests := []struct {
		envVal   string
		fallback string
		want     string
	}{
		{"set_value", "fb", "set_value"},
		{"", "fb", "fb"},
	}
	for _, tt := range tests {
		t.Setenv("TEST_TABLE_KEY", tt.envVal)
		got := getEnv("TEST_TABLE_KEY", tt.fallback)
		if got != tt.want {
			t.Errorf("envVal=%q fallback=%q: got %q, want %q", tt.envVal, tt.fallback, got, tt.want)
		}
	}
}

// ── buildAPIResponse 追加ケース ──────────────────────────────

func TestBuildAPIResponse_400StatusCode(t *testing.T) {
	resp, err := buildAPIResponse(400, map[string]string{"error": "bad request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}

func TestBuildAPIResponse_500StatusCode(t *testing.T) {
	resp, err := buildAPIResponse(500, map[string]string{"error": "internal error"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("got status %d, want 500", resp.StatusCode)
	}
}

func TestBuildAPIResponse_BodyNotEmpty(t *testing.T) {
	resp, _ := buildAPIResponse(200, map[string]string{"key": "value"})
	if resp.Body == "" {
		t.Error("Body should not be empty")
	}
}

func TestBuildAPIResponse_TableDrivenStatusCodes(t *testing.T) {
	for _, code := range []int{200, 400, 500} {
		resp, err := buildAPIResponse(code, map[string]string{})
		if err != nil {
			t.Fatalf("status=%d: unexpected error: %v", code, err)
		}
		if resp.StatusCode != code {
			t.Errorf("status=%d: got %d", code, resp.StatusCode)
		}
	}
}

// ── Handler 400 レスポンスボディ検証 ─────────────────────────

func TestHandler_400BodyContainsErrorKey(t *testing.T) {
	resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: "bad-json"})
	var body map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("400 body is not valid JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("400 body should contain 'error' key, got: %s", resp.Body)
	}
}

func TestHandler_TableDrivenInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"不正JSON", "not-json"},
		{"空body", ""},
		{"空question", `{"question":""}`},
		{"スペースのみ", `{"question":"   "}`},
		{"タブのみ", `{"question":"\t\t"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Handler(context.Background(), events.APIGatewayProxyRequest{Body: tt.body})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != 400 {
				t.Errorf("got status %d, want 400", resp.StatusCode)
			}
		})
	}
}

// ── 構造体シリアライズ検証 ────────────────────────────────────

func TestBedrockBodySerialization(t *testing.T) {
	body := BedrockBody{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        1000,
		System:           "テストシステム",
		Messages:         []Message{{Role: "user", Content: "テスト質問"}},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got BedrockBody
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.AnthropicVersion != "bedrock-2023-05-31" {
		t.Errorf("AnthropicVersion: got %q", got.AnthropicVersion)
	}
	if got.MaxTokens != 1000 {
		t.Errorf("MaxTokens: got %d", got.MaxTokens)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Errorf("Messages: got %+v", got.Messages)
	}
}

func TestResponseSerialization(t *testing.T) {
	r := Response{Answer: "AIの回答", Source: "s3_document"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got Response
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Answer != "AIの回答" {
		t.Errorf("Answer: got %q", got.Answer)
	}
	if got.Source != "s3_document" {
		t.Errorf("Source: got %q", got.Source)
	}
}
