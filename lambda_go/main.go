// aws-rag-knowledgebase: Go 実装（Python 版との並置）
//
// Python 版との比較ポイント:
//   - コールドスタートが Python より高速（バイナリ実行・ランタイム起動なし）
//   - 型安全: 構造体でリクエスト/レスポンスを厳密に定義
//   - init() でクライアントを初期化 → Python のモジュールトップ変数と同等
//   - go.mod でモジュール管理 → requirements.txt に相当
//
// ログとメトリクスは同ディレクトリの logger.go / metrics.go に寄せてある。
// 利用者の質問文と生成された回答がそのまま流れてくるため、素の log.Printf では
// なく logger.go のマスキング（SensitiveKeyPatterns）を必ず通す。
//
// ビルド方法（main.go 単体ではなくパッケージ全体を指定する。
// logger.go / metrics.go / retry.go も同じ package main のため）:
//
//	GOOS=linux GOARCH=arm64 go build -o bootstrap .
//	zip lambda_go.zip bootstrap
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

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

// DefaultMetricsNamespace は METRICS_NAMESPACE 未設定時に使う名前空間。
const DefaultMetricsNamespace = "AwsRagKnowledgebase/QueryHandler"

// リトライのフックに渡す操作名。ディメンションではなくプロパティとして載るので、
// 増やしても課金対象のメトリクス数は増えない。
const (
	RetryOperationGetObject   = "GetObject"
	RetryOperationInvokeModel = "InvokeModel"
)

// ── 環境変数 ──────────────────────────────────────────────
var (
	modelID    = getEnv("BEDROCK_MODEL_ID", "jp.anthropic.claude-haiku-4-5-20251001-v1:0")
	bucketName = getEnv("S3_BUCKET_NAME", "")
	s3Key      = getEnv("S3_PDF_KEY", "documents/knowledge.txt")
)

// ── AWS クライアントインターフェース（テスト時にモックへ差し替え可能） ──
//
// 具象型（*s3.Client / *bedrockruntime.Client）のままだと差し替えられず、
// ハンドラーの正常系をテストできない。並置している aws-bedrock-agent の
// DynamoDBClient と同じ形に揃えてある。
type S3Client interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type BedrockClient interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// ── AWS クライアント（init で初期化・コンテナ再利用時に再生成しない） ──
var (
	s3Client      S3Client
	bedrockClient BedrockClient
)

// リトライ実行器。Bedrock のスロットリングと S3 の一時エラーに備える。
// テストからは Sleep / Rand を差し替えて実待機ゼロで検証する。
var retrier = NewRetrier()

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("AWS 設定の読み込みに失敗: %v", err)
	}
	s3Client = s3.NewFromConfig(cfg)
	bedrockClient = bedrockruntime.NewFromConfig(cfg)
}

// ── 観測可能性（ロガー・メトリクス）の受け渡し ─────────────
//
// getDocumentFromS3 / invokeBedrock の引数を増やさずに、1 回の呼び出しに
// 閉じたロガーとメトリクスを配るため context に載せて運ぶ。パッケージ変数を
// 書き換える方式と違い、呼び出しごとに値が独立するので同時実行で混ざらない。

type loggerCtxKey struct{}

type metricsCtxKey struct{}

// WithObservability は 1 回の呼び出し用のロガーとメトリクスを ctx に載せる。
func WithObservability(ctx context.Context, logger *slog.Logger, metrics *Metrics) context.Context {
	ctx = context.WithValue(ctx, loggerCtxKey{}, logger)
	return context.WithValue(ctx, metricsCtxKey{}, metrics)
}

var (
	fallbackLoggerOnce sync.Once
	fallbackLogger     *slog.Logger
)

// loggerFrom は ctx 上のロガーを返す。
//
// 未設定なら既定ロガーへ落とす。ctx を渡していない呼び出しでもログを
// 落とさないため、nil は返さない。
func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	fallbackLoggerOnce.Do(func() {
		fallbackLogger = NewLoggerFromEnv(LoggerOptions{})
	})
	return fallbackLogger
}

// metricsFrom は ctx 上のメトリクスを返す。未設定なら nil。
func metricsFrom(ctx context.Context) *Metrics {
	m, _ := ctx.Value(metricsCtxKey{}).(*Metrics)
	return m
}

// countMetric はメトリクス未設定でも安全に呼べるカウンタ。
// メトリクスは副次処理なので、出力に失敗しても本処理は止めない。
func countMetric(ctx context.Context, name string) {
	if m := metricsFrom(ctx); m != nil {
		_ = m.Count(name, 1)
	}
}

// retrierWithHooks は ctx のロガー・メトリクスを OnRetry に結線した
// Retrier の「コピー」を返す。
//
// パッケージ変数の retrier を書き換えないのは、同時実行で他の呼び出しに
// 干渉させないため。
func retrierWithHooks(ctx context.Context, op string) Retrier {
	r := retrier
	logHook := RetryLogHook(loggerFrom(ctx), op)
	metricsHook := func(int, time.Duration, error) {}
	if m := metricsFrom(ctx); m != nil {
		metricsHook = RetryMetricsHook(m, op)
	}
	r.OnRetry = func(attempt int, delay time.Duration, err error) {
		logHook(attempt, delay, err)
		metricsHook(attempt, delay, err)
	}
	return r
}

// テストから差し替えるためのフック。
var (
	newHandlerLogger  = func() *slog.Logger { return NewLoggerFromEnv(LoggerOptions{}) }
	newHandlerMetrics = newMetrics
)

// newMetrics は環境変数 METRICS_NAMESPACE を見てメトリクスを組み立てる。
//
// 名前空間が不正なら警告を残して既定へ落とす。メトリクスが作れないことを
// 理由に本処理を止めない。
func newMetrics(logger *slog.Logger) *Metrics {
	ns := os.Getenv("METRICS_NAMESPACE")
	if ns == "" {
		ns = DefaultMetricsNamespace
	}
	if m, err := NewMetrics(MetricsOptions{Namespace: ns}); err == nil {
		return m
	} else {
		logger.Warn("METRICS_NAMESPACE が不正です。既定の名前空間で初期化します",
			"namespace", ns, "error", err)
	}
	// DefaultMetricsNamespace は定数なので、ここで失敗することはない。
	// その不変条件は TestNewMetrics_DefaultNamespaceAlwaysBuilds で固定している。
	m, _ := NewMetrics(MetricsOptions{Namespace: DefaultMetricsNamespace})
	return m
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
	logger := loggerFrom(ctx)

	out, err := RetryValue(ctx, retrierWithHooks(ctx, RetryOperationGetObject), RetryOperationGetObject,
		func(c context.Context) (*s3.GetObjectOutput, error) {
			return s3Client.GetObject(c, &s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(s3Key),
			})
		})
	if err != nil {
		countMetric(ctx, "DocumentFetchError")
		return "", fmt.Errorf("S3 取得エラー: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		countMetric(ctx, "DocumentReadError")
		return "", fmt.Errorf("S3 レスポンス読み込みエラー: %w", err)
	}

	text := string(data)
	truncated := false
	if len(text) > maxDocumentBytes {
		text = text[:maxDocumentBytes] // Python 版と同じ 8000 文字制限
		truncated = true
		countMetric(ctx, "DocumentTruncated")
	}
	// 本文そのものは出さない。社内規定の中身がログへ残らないようにする。
	logger.Info("社内ドキュメントを取得しました",
		"bytes", len(data), "usedBytes", len(text), "truncated", truncated)
	countMetric(ctx, "DocumentFetched")
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
		countMetric(ctx, "BedrockRequestBuildError")
		return "", fmt.Errorf("リクエスト JSON 生成エラー: %w", err)
	}

	stopTimer := func() error { return nil }
	if m := metricsFrom(ctx); m != nil {
		stopTimer = m.Timer("BedrockLatency")
	}
	out, err := RetryValue(ctx, retrierWithHooks(ctx, RetryOperationInvokeModel), RetryOperationInvokeModel,
		func(c context.Context) (*bedrockruntime.InvokeModelOutput, error) {
			return bedrockClient.InvokeModel(c, &bedrockruntime.InvokeModelInput{
				ModelId:     aws.String(modelID),
				Body:        bodyBytes,
				ContentType: aws.String("application/json"),
				Accept:      aws.String("application/json"),
			})
		})
	_ = stopTimer()
	if err != nil {
		countMetric(ctx, "BedrockInvokeError")
		return "", fmt.Errorf("Bedrock 呼び出しエラー: %w", err)
	}

	var result BedrockResponse
	if err := json.Unmarshal(out.Body, &result); err != nil {
		countMetric(ctx, "BedrockResponseParseError")
		return "", fmt.Errorf("Bedrock レスポンス解析エラー: %w", err)
	}
	if len(result.Content) == 0 {
		countMetric(ctx, "BedrockEmptyResponse")
		return "", fmt.Errorf("Bedrock から空の回答が返されました")
	}
	countMetric(ctx, "BedrockInvoked")
	return result.Content[0].Text, nil
}

// ── Lambda ハンドラー ──────────────────────────────────────
func Handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	logger := newHandlerLogger()
	metrics := newHandlerMetrics(logger)
	defer func() {
		// メトリクスは副次処理。出力に失敗しても応答は返す。
		if err := metrics.Flush(); err != nil {
			logger.Warn("メトリクスの出力に失敗しました", "error", err)
		}
	}()

	ctx = WithObservability(ctx, logger, metrics)
	_ = metrics.Count("Invocation", 1)
	stopTimer := metrics.Timer("HandlerLatency")
	defer func() { _ = stopTimer() }()

	var req Request
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil || strings.TrimSpace(req.Question) == "" {
		logger.Warn("リクエストが不正です", "bodyLength", len(event.Body))
		countMetric(ctx, "BadRequest")
		return buildAPIResponse(400, map[string]string{"error": "質問が空または不正です"})
	}
	// 質問文そのものは出さない。利用者の入力が CloudWatch Logs へ
	// 平文で残るのを避けるため、長さだけを残す。
	logger.Info("質問を受信しました", "questionLength", len([]rune(req.Question)))

	docText, err := getDocumentFromS3(ctx)
	if err != nil {
		logger.Warn("社内ドキュメントの取得に失敗しました。一般知識で回答します", "error", err)
	}

	source := "general"
	if docText != "" {
		source = "s3_document"
	}

	answer, err := invokeBedrock(ctx, docText, req.Question)
	if err != nil {
		logger.Error("回答の生成に失敗しました", "error", err, "source", source)
		countMetric(ctx, "AnswerFailed")
		return buildAPIResponse(500, map[string]string{"error": "回答の生成に失敗しました"})
	}

	// 回答本文も出さない。社内ドキュメントの内容が混ざるため。
	logger.Info("回答を生成しました", "source", source, "answerLength", len([]rune(answer)))
	countMetric(ctx, "AnswerGenerated")
	if source == "s3_document" {
		countMetric(ctx, "AnswerFromDocument")
	} else {
		countMetric(ctx, "AnswerFromGeneralKnowledge")
	}
	return buildAPIResponse(200, Response{Answer: answer, Source: source})
}

func main() {
	lambda.Start(Handler)
}
