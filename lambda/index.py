"""
aws-rag-knowledgebase: 社内規定 PDF Q&A Lambda

処理フロー:
  1. API Gateway から質問を受信
  2. S3 からドキュメントテキストを取得（コンテナ再利用時はキャッシュ使用）
  3. Bedrock（Claude）に「ドキュメント内容 + 質問」を送信
  4. 回答を JSON で返す
"""

import json
import os
import sys
import time
from typing import Any

import boto3
from botocore.exceptions import ClientError

sys.path.insert(0, os.path.dirname(__file__))
from logger import StructuredLogger, create_logger_from_env  # noqa: E402
from metrics import MetricsCollector, create_metrics_from_env  # noqa: E402
from retry import retry_call  # noqa: E402
from wiring import RETRY_CONFIG, retry_hooks  # noqa: E402

# ── ロガー ─────────────────────────────────────────────────
# 1 行 JSON で出し、機密キーは自動でマスキングする（logger.py を参照）。
# 社内規定の Q&A なので、質問文・回答文そのものはログに残さない。
logger = create_logger_from_env()

# ── 定数（環境変数から取得） ───────────────────────────────
BEDROCK_MODEL_ID = os.environ.get(
    "BEDROCK_MODEL_ID", "jp.anthropic.claude-haiku-4-5-20251001-v1:0"
)
S3_BUCKET_NAME = os.environ.get("S3_BUCKET_NAME", "")
S3_PDF_KEY = os.environ.get("S3_PDF_KEY", "documents/knowledge.txt")
AWS_REGION = os.environ.get("AWS_REGION", "ap-northeast-1")

SYSTEM_PROMPT = """あなたは社内規定・ポリシーの専門アシスタントです。
提供された社内ドキュメントの内容に基づいて、質問に正確・簡潔に回答してください。
ドキュメントに記載がない場合は「この内容はドキュメントに記載がありません。担当部署にご確認ください。」と答えてください。
個人情報や機密情報には触れないでください。"""

# ── クライアント初期化（コンテナ再利用時に再生成しない） ──
_s3_client = boto3.client("s3", region_name=AWS_REGION)
_bedrock_client = boto3.client("bedrock-runtime", region_name=AWS_REGION)

# ── S3 ドキュメントキャッシュ（同一コンテナ内で再利用） ──
_cached_document: str | None = None


# ── S3 からテキスト取得 ────────────────────────────────────
def get_document_from_s3(
    bucket: str,
    key: str,
    *,
    log: StructuredLogger | None = None,
    metrics: MetricsCollector | None = None,
) -> str:
    """S3 からドキュメントテキストを取得する（キャッシュ付き）

    SlowDown（S3 のスロットリング）に備えてリトライ付きで呼び出す。
    """
    global _cached_document
    log = log or logger
    if _cached_document is not None:
        log.info("S3 のキャッシュを使用します", cached=True)
        if metrics is not None:
            metrics.add_metric("S3CacheHits", 1, unit="Count")
        return _cached_document

    try:
        response = retry_call(
            _s3_client.get_object,
            Bucket=bucket,
            Key=key,
            config=RETRY_CONFIG,
            on_retry=retry_hooks("S3.GetObject", log, metrics),
        )
        content = response["Body"].read().decode("utf-8")
        log.info(
            "S3 からドキュメントを取得しました",
            s3_bucket=bucket,
            s3_key=key,
            characters=len(content),
        )
        if metrics is not None:
            metrics.add_metric("S3CacheMisses", 1, unit="Count")
            metrics.add_metric("DocumentCharacters", len(content), unit="Count")
        _cached_document = content
        return content
    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        log.error("S3 の取得に失敗しました", error_code=error_code, s3_key=key, error=e)
        if metrics is not None:
            metrics.add_metric("S3Errors", 1, unit="Count")
        if error_code == "NoSuchKey":
            return ""
        raise


# ── Bedrock 呼び出し ───────────────────────────────────────
def invoke_bedrock(
    document_text: str,
    question: str,
    *,
    log: StructuredLogger | None = None,
    metrics: MetricsCollector | None = None,
) -> str:
    """
    S3 から取得したドキュメント内容を Claude のコンテキストに渡して回答を生成する。
    これが「RAG の簡易実装」: ドキュメント全文をプロンプトに埋め込む。
    TODO: 文書が大きい場合はチャンク分割・類似度検索（本格 RAG）に切り替える
    """
    if document_text:
        # トークン節約のため先頭 8000 文字に制限
        user_message = f"""以下の社内ドキュメントを参照して質問に答えてください。

【社内ドキュメント】
{document_text[:8000]}

【質問】
{question}"""
    else:
        user_message = f"""社内ドキュメントが見つかりませんでした。
一般的な知識で以下の質問に回答してください。

【質問】
{question}"""

    body = json.dumps(
        {
            "anthropic_version": "bedrock-2023-05-31",
            "max_tokens": 1000,
            "system": SYSTEM_PROMPT,
            "messages": [{"role": "user", "content": user_message}],
        }
    )

    log = log or logger
    try:
        # ThrottlingException / ModelNotReadyException は retry.py が
        # 指数バックオフ + フルジッターで自動リトライする。
        # ここまで来た例外は、リトライしても回復しなかったものだけ。
        response = retry_call(
            _bedrock_client.invoke_model,
            modelId=BEDROCK_MODEL_ID,
            body=body,
            contentType="application/json",
            accept="application/json",
            config=RETRY_CONFIG,
            on_retry=retry_hooks("Bedrock.InvokeModel", log, metrics),
        )
        result = json.loads(response["body"].read())
        answer = result["content"][0]["text"]
        log.info(
            "Bedrock が回答を生成しました",
            model_id=BEDROCK_MODEL_ID,
            answer_length=len(answer),
        )
        if metrics is not None:
            metrics.add_metric("BedrockSuccess", 1, unit="Count")
            metrics.add_metric("AnswerCharacters", len(answer), unit="Count")
        return answer

    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        log.error("Bedrock がエラーを返しました", error_code=error_code, error=e)
        if metrics is not None:
            metrics.add_metric("BedrockErrors", 1, unit="Count")
            metrics.set_property("last_bedrock_error", error_code)
        if error_code == "AccessDeniedException":
            return "AI 機能が利用できません。管理者にご連絡ください。"
        elif error_code == "ThrottlingException":
            # リトライを使い切ってなお失敗した場合のみここに来る
            return "リクエストが集中しています。しばらくしてから再度お試しください。"
        else:
            return "回答の生成に失敗しました。担当部署にご確認ください。"


# ── Lambda ハンドラー ──────────────────────────────────────
def handler(event: dict[str, Any], context: Any) -> dict[str, Any]:
    """
    API Gateway からのリクエストを処理する。
    リクエストボディ: {"question": "質問テキスト"}
    レスポンス: {"answer": "回答テキスト", "source": "s3_document or general"}
    """
    request_id = getattr(context, "aws_request_id", None)
    log = logger.child(request_id=request_id) if request_id else logger
    metrics = create_metrics_from_env()
    metrics.set_dimensions(
        FunctionName=os.environ.get("AWS_LAMBDA_FUNCTION_NAME", "local")
    )
    started = time.monotonic()
    log.info("リクエストを受信しました")

    def finish(response: dict[str, Any]) -> dict[str, Any]:
        """レスポンスを返す直前に、必ずメトリクスを出し切る。"""
        metrics.add_metric(
            "HandlerDuration", (time.monotonic() - started) * 1000, unit="Milliseconds"
        )
        metrics.add_metric("StatusCode", response["statusCode"], unit="None")
        try:
            metrics.flush()
        except Exception as exc:  # pragma: no cover - 想定外のみ
            log.warn("メトリクスの出力に失敗しました", error=exc)
        return response

    # ── リクエスト解析 ─────────────────────────────────────
    try:
        body_str = event.get("body", "{}")
        if isinstance(body_str, str):
            body = json.loads(body_str)
        else:
            body = body_str
    except json.JSONDecodeError:
        log.error("リクエストボディを JSON として読めませんでした")
        metrics.add_metric("BadRequests", 1, unit="Count")
        return finish(
            {
                "statusCode": 400,
                "headers": {
                    "Content-Type": "application/json",
                    "Access-Control-Allow-Origin": "*",
                },
                "body": json.dumps({"error": "Invalid JSON"}, ensure_ascii=False),
            }
        )

    question = body.get("question", "").strip()
    if not question:
        log.warn("質問が空のリクエストを受け取りました")
        metrics.add_metric("BadRequests", 1, unit="Count")
        return finish(
            {
                "statusCode": 400,
                "headers": {
                    "Content-Type": "application/json",
                    "Access-Control-Allow-Origin": "*",
                },
                "body": json.dumps({"error": "質問が空です"}, ensure_ascii=False),
            }
        )

    # 質問文そのものは残さない。社内規定の Q&A なので、
    # 質問の中に個人名や処遇の相談が含まれうる（長さだけ記録する）。
    log.info("質問を受信しました", question_length=len(question))
    metrics.add_metric("QuestionCharacters", len(question), unit="Count")

    # ── S3 からドキュメント取得 ────────────────────────────
    document_text = ""
    source = "general"
    if S3_BUCKET_NAME:
        document_text = get_document_from_s3(
            S3_BUCKET_NAME, S3_PDF_KEY, log=log, metrics=metrics
        )
        if document_text:
            source = "s3_document"

    # ── Bedrock で回答生成 ────────────────────────────────
    answer = invoke_bedrock(document_text, question, log=log, metrics=metrics)

    # 回答文も残さない（ドキュメントの中身がそのまま出てしまうため）
    log.info("回答を生成しました", source=source, answer_length=len(answer))
    metrics.set_property("source", source)
    metrics.add_metric("AnswersReturned", 1, unit="Count")

    return finish(
        {
            "statusCode": 200,
            "headers": {
                "Content-Type": "application/json",
                "Access-Control-Allow-Origin": "*",
            },
            "body": json.dumps(
                {"answer": answer, "source": source}, ensure_ascii=False
            ),
        }
    )
