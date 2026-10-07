package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ── モック AWS クライアント ──────────────────────────────────
//
// s3Client / bedrockClient をインターフェースにしたので、ハンドラーの
// 正常系をネットワークなしで通せるようになった。

type mockS3 struct {
	body       string
	err        error
	callCount  int
	lastBucket string
}

func (m *mockS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.callCount++
	if params != nil && params.Bucket != nil {
		m.lastBucket = *params.Bucket
	}
	if m.err != nil {
		return nil, m.err
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(m.body))}, nil
}

type mockBedrock struct {
	answer    string
	rawBody   []byte
	err       error
	callCount int
}

func (m *mockBedrock) InvokeModel(_ context.Context, _ *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if m.rawBody != nil {
		return &bedrockruntime.InvokeModelOutput{Body: m.rawBody}, nil
	}
	b, _ := json.Marshal(BedrockResponse{
		Content: []struct {
			Text string `json:"text"`
		}{{Text: m.answer}},
	})
	return &bedrockruntime.InvokeModelOutput{Body: b}, nil
}

// ── テスト用ヘルパー ──────────────────────────────────────────
//
// ここでのテストの主眼は「logger.go / metrics.go が main.go から実際に
// 呼ばれているか」。関数単体の挙動は logger_test.go / metrics_test.go 側で
// 検証済みなので、ここでは結線が切れたら落ちることだけを固定する。

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return NewLogger(LoggerOptions{Level: LogLevelDebug, Writer: buf})
}

func newTestMetrics(t *testing.T, buf *bytes.Buffer) *Metrics {
	t.Helper()
	m, err := NewMetrics(MetricsOptions{Namespace: "Test/Observability", Sink: buf})
	if err != nil {
		t.Fatalf("NewMetrics failed: %v", err)
	}
	return m
}

// withMockAWS は AWS クライアントとバケット名を差し替えて元に戻す。
func withMockAWS(s3m S3Client, brm BedrockClient, bucket string, fn func()) {
	origS3, origBR, origBucket := s3Client, bedrockClient, bucketName
	s3Client, bedrockClient, bucketName = s3m, brm, bucket
	defer func() { s3Client, bedrockClient, bucketName = origS3, origBR, origBucket }()
	fn()
}

// withNoSleepRetrier はパッケージ変数の retrier を実待機ゼロに差し替える。
func withNoSleepRetrier(fn func()) {
	orig := retrier
	retrier.Sleep = func(context.Context, time.Duration) error { return nil }
	defer func() { retrier = orig }()
	fn()
}

// withTestObservability はハンドラーのロガー・メトリクス生成を差し替える。
func withTestObservability(t *testing.T, logBuf, metricBuf *bytes.Buffer, fn func(*Metrics)) {
	t.Helper()
	metrics := newTestMetrics(t, metricBuf)
	origLogger, origMetrics := newHandlerLogger, newHandlerMetrics
	newHandlerLogger = func() *slog.Logger { return newTestLogger(logBuf) }
	newHandlerMetrics = func(*slog.Logger) *Metrics { return metrics }
	defer func() { newHandlerLogger, newHandlerMetrics = origLogger, origMetrics }()
	fn(metrics)
}

func apiRequest(question string) events.APIGatewayProxyRequest {
	b, _ := json.Marshal(Request{Question: question})
	return events.APIGatewayProxyRequest{Body: string(b)}
}

// ── Retrier.OnRetry ───────────────────────────────────────────

func TestRetrier_OnRetryHookIsCalled(t *testing.T) {
	calls := 0
	r := NewRetrier()
	r.Sleep = func(context.Context, time.Duration) error { return nil }
	r.OnRetry = func(attempt int, _ time.Duration, err error) {
		calls++
		if attempt < 1 {
			t.Errorf("attempt should start at 1, got %d", attempt)
		}
		if err == nil {
			t.Error("OnRetry should receive the error that triggered the retry")
		}
	}

	if err := r.Do(context.Background(), "GetObject", func(context.Context) error {
		return retryTestThrottling
	}); err == nil {
		t.Fatal("expected the final error to be returned")
	}
	if want := r.Config.MaxAttempts - 1; calls != want {
		t.Errorf("OnRetry called %d times, want %d", calls, want)
	}
}

func TestRetrier_OnRetryNilDoesNotPanic(t *testing.T) {
	r := NewRetrier()
	r.Sleep = func(context.Context, time.Duration) error { return nil }
	// OnRetry は nil のまま。従来どおり標準ログへ出力され、落ちないこと。
	if err := r.Do(context.Background(), "GetObject", func(context.Context) error {
		return retryTestThrottling
	}); err == nil {
		t.Fatal("expected the final error to be returned")
	}
}

// ── context 経由のロガー / メトリクス ────────────────────────

func TestLoggerFrom_ReturnsContextLogger(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithObservability(context.Background(), newTestLogger(&buf), nil)

	loggerFrom(ctx).Info("結線の確認")

	if !strings.Contains(buf.String(), "結線の確認") {
		t.Errorf("context のロガーが使われていない: %s", buf.String())
	}
}

func TestLoggerFrom_FallsBackWhenAbsent(t *testing.T) {
	if loggerFrom(context.Background()) == nil {
		t.Fatal("loggerFrom should never return nil")
	}
}

func TestMetricsFrom_ReturnsNilWhenAbsent(t *testing.T) {
	if metricsFrom(context.Background()) != nil {
		t.Error("metricsFrom should return nil when unset")
	}
}

func TestCountMetric_NoMetricsIsNoop(t *testing.T) {
	// メトリクス未設定でも panic しないこと。副次処理で本処理を止めないため。
	countMetric(context.Background(), "Anything")
}

func TestNewMetrics_DefaultNamespaceAlwaysBuilds(t *testing.T) {
	// main.go の newMetrics は「既定の名前空間なら必ず作れる」前提で
	// エラーを握りつぶしている。その不変条件をここで固定する。
	if _, err := NewMetrics(MetricsOptions{Namespace: DefaultMetricsNamespace}); err != nil {
		t.Fatalf("DefaultMetricsNamespace must always build, got: %v", err)
	}
}

// ── retrierWithHooks：ロガーとメトリクスの両方へ流れるか ──────

func TestRetrierWithHooks_WiresBothLoggerAndMetrics(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	metrics := newTestMetrics(t, &metricBuf)
	ctx := WithObservability(context.Background(), newTestLogger(&logBuf), metrics)

	withNoSleepRetrier(func() {
		r := retrierWithHooks(ctx, RetryOperationInvokeModel)
		_ = r.Do(ctx, RetryOperationInvokeModel, func(context.Context) error {
			return retryTestThrottling
		})
	})
	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if !strings.Contains(logBuf.String(), RetryOperationInvokeModel) {
		t.Errorf("RetryLogHook が結線されていない: %s", logBuf.String())
	}
	if got := metricBuf.String(); !strings.Contains(got, "RetryAttempt") {
		t.Errorf("RetryMetricsHook が結線されていない: %s", got)
	}
}

// ── Handler：正常系でメトリクスが出るか ───────────────────────

func TestHandler_EmitsMetricsOnSuccess(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer

	withTestObservability(t, &logBuf, &metricBuf, func(*Metrics) {
		withMockAWS(&mockS3{body: "就業規則の本文"}, &mockBedrock{answer: "回答です"}, "test-bucket", func() {
			resp, err := Handler(context.Background(), apiRequest("有給は何日ですか"))
			if err != nil {
				t.Fatalf("Handler returned an error: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("StatusCode = %d, want 200 (body=%s)", resp.StatusCode, resp.Body)
			}
			if !strings.Contains(resp.Body, "s3_document") {
				t.Errorf("source が s3_document になっていない: %s", resp.Body)
			}
		})
	})

	got := metricBuf.String()
	for _, name := range []string{
		"Invocation", "HandlerLatency", "DocumentFetched",
		"BedrockInvoked", "BedrockLatency", "AnswerGenerated", "AnswerFromDocument",
	} {
		if !strings.Contains(got, name) {
			t.Errorf("メトリクス %s が出力されていない: %s", name, got)
		}
	}
}

func TestHandler_CountsBadRequest(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer

	withTestObservability(t, &logBuf, &metricBuf, func(*Metrics) {
		resp, _ := Handler(context.Background(), events.APIGatewayProxyRequest{Body: "bad-json"})
		if resp.StatusCode != 400 {
			t.Fatalf("StatusCode = %d, want 400", resp.StatusCode)
		}
	})

	if got := metricBuf.String(); !strings.Contains(got, "BadRequest") {
		t.Errorf("BadRequest が出力されていない: %s", got)
	}
}

func TestHandler_CountsAnswerFailedOnBedrockError(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer

	withNoSleepRetrier(func() {
		withTestObservability(t, &logBuf, &metricBuf, func(*Metrics) {
			withMockAWS(&mockS3{}, &mockBedrock{err: retryTestThrottling}, "", func() {
				resp, _ := Handler(context.Background(), apiRequest("質問"))
				if resp.StatusCode != 500 {
					t.Fatalf("StatusCode = %d, want 500", resp.StatusCode)
				}
			})
		})
	})

	got := metricBuf.String()
	for _, name := range []string{"BedrockInvokeError", "AnswerFailed"} {
		if !strings.Contains(got, name) {
			t.Errorf("メトリクス %s が出力されていない: %s", name, got)
		}
	}
}

func TestHandler_CountsGeneralKnowledgeWhenNoBucket(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer

	withTestObservability(t, &logBuf, &metricBuf, func(*Metrics) {
		// バケット未設定 → S3 を呼ばずに一般知識で回答する経路。
		s3m := &mockS3{}
		withMockAWS(s3m, &mockBedrock{answer: "一般的な回答"}, "", func() {
			resp, _ := Handler(context.Background(), apiRequest("質問"))
			if resp.StatusCode != 200 {
				t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
			}
			if s3m.callCount != 0 {
				t.Errorf("バケット未設定なのに S3 を呼んでいる: %d 回", s3m.callCount)
			}
		})
	})

	if got := metricBuf.String(); !strings.Contains(got, "AnswerFromGeneralKnowledge") {
		t.Errorf("AnswerFromGeneralKnowledge が出力されていない: %s", got)
	}
}

// ── ★ 利用者の質問文・回答・社内ドキュメントをログへ残さない ──

func TestHandler_DoesNotLogSensitiveText(t *testing.T) {
	const (
		question = "質問ZZZ-この文字列はログに出てはいけない"
		document = "社外秘YYY-この文字列はログに出てはいけない"
		answer   = "回答XXX-この文字列はログに出てはいけない"
	)

	var logBuf, metricBuf bytes.Buffer
	withTestObservability(t, &logBuf, &metricBuf, func(*Metrics) {
		withMockAWS(&mockS3{body: document}, &mockBedrock{answer: answer}, "test-bucket", func() {
			if _, err := Handler(context.Background(), apiRequest(question)); err != nil {
				t.Fatalf("Handler returned an error: %v", err)
			}
		})
	})

	logged := logBuf.String()
	for _, secret := range []string{question, document, answer} {
		if strings.Contains(logged, secret) {
			t.Errorf("機密になりうる本文がログへ出ている: %s", logged)
		}
	}
	// 長さは残しているので、追跡はできること。
	for _, key := range []string{"questionLength", "answerLength"} {
		if !strings.Contains(logged, key) {
			t.Errorf("%s が記録されていない: %s", key, logged)
		}
	}
}

// ── S3 の切り詰め ─────────────────────────────────────────────

func TestGetDocumentFromS3_CountsTruncation(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	metrics := newTestMetrics(t, &metricBuf)
	ctx := WithObservability(context.Background(), newTestLogger(&logBuf), metrics)

	long := strings.Repeat("a", maxDocumentBytes+100)
	withMockAWS(&mockS3{body: long}, &mockBedrock{}, "test-bucket", func() {
		text, err := getDocumentFromS3(ctx)
		if err != nil {
			t.Fatalf("getDocumentFromS3 failed: %v", err)
		}
		if len(text) != maxDocumentBytes {
			t.Errorf("len(text) = %d, want %d", len(text), maxDocumentBytes)
		}
	})
	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if got := metricBuf.String(); !strings.Contains(got, "DocumentTruncated") {
		t.Errorf("DocumentTruncated が出力されていない: %s", got)
	}
	// 本文そのものはログに出ていないこと。
	if strings.Contains(logBuf.String(), strings.Repeat("a", 100)) {
		t.Error("ドキュメント本文がログへ出ている")
	}
}
