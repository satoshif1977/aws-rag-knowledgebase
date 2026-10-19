"""
EventBridge Pipes 経由で呼び出されるドキュメント取り込みハンドラー

フロー:
  SQS（S3 ObjectCreated イベント通知）
    → EventBridge Pipes（.pdf/.txt/.md/.docx のみフィルター通過）
      → このハンドラー
        → S3 からドキュメントのメタ情報を取得して検証
        → ログ出力（実運用の拡張ポイント: KB 同期・DynamoDB 登録・SNS 通知）

EventBridge Pipes + Lambda の event 形式:
  Pipes は SQS メッセージをリスト形式で渡す（batch_size=1 なら 1 要素）
  [{"messageId": "...", "body": "<JSON 文字列>", ...}]
  body は S3 イベント通知の JSON 文字列（Records 配列を含む）
"""

import json
import os
import sys
import urllib.parse
from typing import Any

import boto3
from botocore.exceptions import ClientError

sys.path.insert(0, os.path.dirname(__file__))
from logger import StructuredLogger, create_logger_from_env  # noqa: E402
from metrics import MetricsCollector, create_metrics_from_env  # noqa: E402
from retry import retry_call  # noqa: E402
from wiring import RETRY_CONFIG, retry_hooks  # noqa: E402

# ── ロガー ─────────────────────────────────────────────────────────────
# index.py と同じ 1 行 JSON。S3 キーはフィールドで渡してマスキングを効かせる。
logger = create_logger_from_env()

# ── サポート対象の拡張子（Pipes フィルターと一致させる） ──────────────
SUPPORTED_EXTENSIONS = frozenset({".pdf", ".txt", ".md", ".docx"})

# ── クライアント初期化（コンテナ再利用で再生成しない） ────────────────
_s3_client = boto3.client(
    "s3", region_name=os.environ.get("AWS_REGION", "ap-northeast-1")
)


def _get_extension(key: str) -> str:
    """オブジェクトキーからファイル拡張子を取得する（小文字・ドット含む）"""
    _, ext = os.path.splitext(key.lower())
    return ext


def _process_s3_record(
    record: dict[str, Any],
    *,
    log: StructuredLogger | None = None,
    metrics: MetricsCollector | None = None,
) -> dict[str, Any]:
    """S3 イベントレコードを1件処理してメタ情報を返す"""
    bucket = record["s3"]["bucket"]["name"]
    # S3 イベント通知のキーは URL エンコードされているためデコードする
    key = urllib.parse.unquote_plus(record["s3"]["object"]["key"])
    size = record["s3"]["object"].get("size", 0)
    ext = _get_extension(key)
    log = log or logger

    log.info(
        "ドキュメントの取り込みを開始します",
        s3_bucket=bucket,
        s3_key=key,
        size_bytes=size,
        extension=ext,
    )

    # 拡張子の二重確認（Pipes フィルターをすり抜けた非対応ファイルへの保険）
    if ext not in SUPPORTED_EXTENSIONS:
        log.warn("サポート外の拡張子なのでスキップします", s3_key=key, extension=ext)
        return {
            "key": key,
            "status": "skipped",
            "reason": f"unsupported extension: {ext}",
        }

    try:
        # Head ではなく GetObject でメタ情報と先頭バイトを取得（疎通確認）
        # SlowDown（S3 のスロットリング）に備えてリトライ付きで呼ぶ
        response = retry_call(
            _s3_client.get_object,
            Bucket=bucket,
            Key=key,
            config=RETRY_CONFIG,
            on_retry=retry_hooks("S3.GetObject", log, metrics),
        )
        content_length = response["ContentLength"]
        content_type = response.get("ContentType", "application/octet-stream")

        log.info(
            "ドキュメントを取り込みました",
            s3_key=key,
            s3_bucket=bucket,
            size_bytes=content_length,
            content_type=content_type,
        )
        if metrics is not None:
            metrics.add_metric("DocumentBytes", content_length, unit="Bytes")

        # ── 実運用での拡張ポイント ─────────────────────────────────────
        # 以下のいずれかに拡張することで本格的な RAG パイプラインを構築できる:
        #   1. bedrock-agent: start_ingestion_job() で Knowledge Base を同期
        #   2. DynamoDB にドキュメントインデックスを登録
        #   3. SNS/EventBridge で後続処理（テキスト抽出・チャンク分割）をトリガー
        # ──────────────────────────────────────────────────────────────

        return {
            "key": key,
            "bucket": bucket,
            "size": content_length,
            "content_type": content_type,
            "status": "success",
        }

    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        log.error("S3 の取得に失敗しました", error_code=error_code, s3_key=key, error=e)
        return {"key": key, "status": "error", "reason": error_code}


def handler(
    event: list[dict[str, Any]] | dict[str, Any],
    context: Any,
) -> dict[str, Any]:
    """
    EventBridge Pipes から呼び出されるエントリーポイント。
    batch_size=1 のため通常は 1 要素のリストを受け取るが、複数要素にも対応する。
    直接 Lambda を invoke した場合（dict 形式）にも対応する。
    """
    records = event if isinstance(event, list) else [event]
    request_id = getattr(context, "aws_request_id", None)
    log = logger.child(request_id=request_id) if request_id else logger
    metrics = create_metrics_from_env()
    metrics.set_dimensions(
        FunctionName=os.environ.get("AWS_LAMBDA_FUNCTION_NAME", "local")
    )
    log.info("取り込みを開始します", record_count=len(records))
    metrics.add_metric("SqsRecords", len(records), unit="Count")

    processed: list[dict[str, Any]] = []  # 取り込み成功
    skipped: list[dict[str, Any]] = []  # 拡張子不一致でスキップ
    errors: list[dict[str, Any]] = []  # S3 エラー等

    for sqs_record in records:
        try:
            # SQS メッセージの body は JSON 文字列（S3 イベント通知）
            body_raw = sqs_record.get("body", "{}")
            body = json.loads(body_raw) if isinstance(body_raw, str) else body_raw

            for s3_record in body.get("Records", []):
                result = _process_s3_record(s3_record, log=log, metrics=metrics)
                status = result.get("status")
                if status == "success":
                    processed.append(result)
                elif status == "skipped":
                    skipped.append(result)
                else:
                    errors.append(result)

        except (json.JSONDecodeError, KeyError) as e:
            log.error("SQS メッセージを解析できませんでした", error=e)
            errors.append({"status": "error", "reason": str(e)})

    log.info(
        "取り込みが完了しました",
        processed=len(processed),
        skipped=len(skipped),
        errors=len(errors),
    )
    metrics.add_metric("DocumentsProcessed", len(processed), unit="Count")
    metrics.add_metric("DocumentsSkipped", len(skipped), unit="Count")
    metrics.add_metric("DocumentsFailed", len(errors), unit="Count")
    # メトリクスは副次処理。ここで失敗しても取り込み結果は返す
    try:
        metrics.flush()
    except Exception as exc:  # pragma: no cover - 想定外のみ
        log.warn("メトリクスの出力に失敗しました", error=exc)

    return {
        "processed": processed,
        "skipped": skipped,
        "errors": errors,
        "total": len(processed),
    }
