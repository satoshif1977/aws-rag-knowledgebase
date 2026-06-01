// aws-rag-knowledgebase: Go 実装（Python 版との並置）
//
// Python 版との比較ポイント:
//   - コールドスタートが Python より高速（バイナリ実行・ランタイム起動なし）
//   - 型安全: 構造体でリクエスト/レスポンスを厳密に定義
//   - init() でクライアントを初期化 → Python のモジュールトップ変数と同等
//   - go.mod でモジュール管理 → requirements.txt に相当
//
// ビルド方法:
//   GOOS=linux GOARCH=arm64 go build -o bootstrap main.go
//   zip lambda_go.zip bootstrap
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ── 定数 ─────────────────────────────────────────────────
const (
	maxDocumentBytes = 8000
	systemPrompt     = `あなたは社内規定・ポリシーの専門アシスタントです。` +
		`提供された社内ドキュメントの内容に基づいて、質問に正確・簡潔に回答してください。` +
		`ドキュメントに記載がない場合は「この内容はドキュメントに記載がありません。担当部署にご確認ください。」と答えてください。`
)

// ── 環境変数 ──────────────────────────────────────────────
var (
	modelID    = getEnv("BEDROCK_MODEL_ID", "jp.anthropic.claude-haiku-4-5-20251001-v1:0")
	bucketName = getEnv("S3_BUCKET_NAME", "")
	s3Key      = getEnv("S3_PDF_KEY", "documents/knowledge.txt")
)

// ── AWS クライアント（init で初期化・コンテナ再利用時に再生成しない） ──
var (
	s3Client      *s3.Client
	bedrockClient *bedrockruntime.Client
)

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("AWS 設定の読み込みに失敗: %v", err)
	}
	s3Client = s3.NewFromConfig(cfg)
	bedrockClient = bedrockruntime.NewFromConfig(cfg)
}

// ── リクエスト / レスポンス型 ────────────────────────────
type Request struct {
	Question string `json:"question"`
}

type Response struct {
	Answer string `json:"answer"`
	Source string `json:"source"`
}

type BedrockBody struct {
	AnthropicVersion string    `json:"anthropic_version"`
	MaxTokens        int       `json:"max_tokens"`
	System           string    `json:"system"`
	Messages         []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type BedrockResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// ── ヘルパー ─────────────────────────────────────────────
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildAPIResponse(statusCode int, body any) (events.APIGatewayProxyResponse, error) {
	b, _ := json.Marshal(body)
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(b),
	}, nil
}

// ── S3 からドキュメント取得 ───────────────────────────────
func getDocumentFromS3(ctx context.Context) (string, error) {
	if bucketName == "" {
		return "", nil
	}
	out, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return "", fmt.Errorf("S3 取得エラー: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return "", fmt.Errorf("S3 レスポンス読み込みエラー: %w", err)
	}

	text := string(data)
	if len(text) > maxDocumentBytes {
		text = text[:maxDocumentBytes] // Python 版と同じ 8000 文字制限
	}
	return text, nil
}

// ── Bedrock 呼び出し ──────────────────────────────────────
func invokeBedrock(ctx context.Context, docText, question string) (string, error) {
	var userMsg string
	if docText != "" {
		userMsg = fmt.Sprintf(
			"以下の社内ドキュメントを参照して質問に答えてください。\n\n【社内ドキュメント】\n%s\n\n【質問】\n%s",
			docText, question,
		)
	} else {
		userMsg = fmt.Sprintf(
			"社内ドキュメントが見つかりませんでした。一般的な知識で以下の質問に回答してください。\n\n【質問】\n%s",
			question,
		)
	}

	bodyBytes, err := json.Marshal(BedrockBody{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        1000,
		System:           systemPrompt,
		Messages:         []Message{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		return "", fmt.Errorf("リクエスト JSON 生成エラー: %w", err)
	}

	out, err := bedrockClient.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Body:        bodyBytes,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return "", fmt.Errorf("Bedrock 呼び出しエラー: %w", err)
	}

	var result BedrockResponse
	if err := json.Unmarshal(out.Body, &result); err != nil {
		return "", fmt.Errorf("Bedrock レスポンス解析エラー: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("Bedrock から空の回答が返されました")
	}
	return result.Content[0].Text, nil
}

// ── Lambda ハンドラー ──────────────────────────────────────
func Handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req Request
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil || strings.TrimSpace(req.Question) == "" {
		return buildAPIResponse(400, map[string]string{"error": "質問が空または不正です"})
	}
	log.Printf("質問受信: %.50s...", req.Question)

	docText, err := getDocumentFromS3(ctx)
	if err != nil {
		log.Printf("S3 取得エラー（一般知識で回答）: %v", err)
	}

	source := "general"
	if docText != "" {
		source = "s3_document"
	}

	answer, err := invokeBedrock(ctx, docText, req.Question)
	if err != nil {
		log.Printf("Bedrock エラー: %v", err)
		return buildAPIResponse(500, map[string]string{"error": "回答の生成に失敗しました"})
	}

	log.Printf("回答生成完了 (source=%s): %.50s...", source, answer)
	return buildAPIResponse(200, Response{Answer: answer, Source: source})
}

func main() {
	lambda.Start(Handler)
}
