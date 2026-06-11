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
