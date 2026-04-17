"""
aws-rag-knowledgebase: 社内規定 PDF Q&A Lambda

処理フロー:
  1. API Gateway から質問を受信
  2. SSM Parameter Store から設定値取得
  3. S3 から PDF テキストを取得
  4. Bedrock（Claude）に「PDF内容 + 質問」を送信
  5. 回答を JSON で返す
"""

import json
import logging
import os
import boto3
from botocore.exceptions import ClientError

# ── ロガー設定 ─────────────────────────────────────────────
logger = logging.getLogger()
logger.setLevel(os.environ.get("LOG_LEVEL", "INFO"))

# ── 定数（環境変数から取得） ───────────────────────────────
BEDROCK_MODEL_ID = os.environ.get(
    "BEDROCK_MODEL_ID", "anthropic.claude-3-haiku-20240307-v1:0"
)
S3_BUCKET_NAME = os.environ.get("S3_BUCKET_NAME", "")
S3_PDF_KEY = os.environ.get("S3_PDF_KEY", "documents/knowledge.txt")
AWS_REGION = os.environ.get("AWS_REGION", "ap-northeast-1")

SYSTEM_PROMPT = """あなたは社内規定・ポリシーの専門アシスタントです。
提供された社内ドキュメントの内容に基づいて、質問に正確・簡潔に回答してください。
ドキュメントに記載がない場合は「この内容はドキュメントに記載がありません。担当部署にご確認ください。」と答えてください。
個人情報や機密情報には触れないでください。"""


# ── S3 からテキスト取得 ────────────────────────────────────
def get_document_from_s3(bucket: str, key: str) -> str:
    """S3 からドキュメントテキストを取得する"""
    try:
        s3_client = boto3.client("s3", region_name=AWS_REGION)
        response = s3_client.get_object(Bucket=bucket, Key=key)
        content = response["Body"].read().decode("utf-8")
        logger.info(
            f"S3 ドキュメント取得成功: s3://{bucket}/{key} ({len(content)} 文字)"
        )
        return content
    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        logger.error(f"S3 取得エラー: {error_code} / {e}")
        if error_code == "NoSuchKey":
            return ""
        raise


# ── Bedrock 呼び出し ───────────────────────────────────────
def invoke_bedrock(document_text: str, question: str) -> str:
    """
    S3 から取得したドキュメント内容を Claude のコンテキストに渡して回答を生成する。
    これが「RAG の簡易実装」: ドキュメント全文をプロンプトに埋め込む。
    TODO: 文書が大きい場合はチャンク分割・類似度検索（本格 RAG）に切り替える
    """
    bedrock_client = boto3.client("bedrock-runtime", region_name=AWS_REGION)

    # ドキュメントが取得できた場合のみコンテキストに追加
    if document_text:
        user_message = f"""以下の社内ドキュメントを参照して質問に答えてください。

【社内ドキュメント】
{document_text[:8000]}  # トークン節約のため先頭 8000 文字に制限

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

    try:
        response = bedrock_client.invoke_model(
            modelId=BEDROCK_MODEL_ID,
            body=body,
            contentType="application/json",
            accept="application/json",
        )
        result = json.loads(response["body"].read())
        answer = result["content"][0]["text"]
        logger.info(f"Bedrock 回答生成成功: model={BEDROCK_MODEL_ID}")
        return answer

    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        logger.error(f"Bedrock エラー: {error_code} / {e}")
        if error_code == "AccessDeniedException":
            return "AI 機能が利用できません。管理者にご連絡ください。"
        elif error_code == "ThrottlingException":
            return "リクエストが集中しています。しばらくしてから再度お試しください。"
        else:
            return "回答の生成に失敗しました。担当部署にご確認ください。"


# ── Lambda ハンドラー ──────────────────────────────────────
def handler(event: dict, context) -> dict:
    """
    API Gateway からのリクエストを処理する。
    リクエストボディ: {"question": "質問テキスト"}
    レスポンス: {"answer": "回答テキスト", "source": "s3 or general"}
    """
    logger.info("Lambda 起動")

    # ── リクエスト解析 ─────────────────────────────────────
    try:
        body_str = event.get("body", "{}")
        if isinstance(body_str, str):
            body = json.loads(body_str)
        else:
            body = body_str
    except json.JSONDecodeError:
        logger.error("リクエストボディの JSON パース失敗")
        return {
            "statusCode": 400,
            "headers": {"Content-Type": "application/json"},
            "body": json.dumps({"error": "Invalid JSON"}, ensure_ascii=False),
        }

    question = body.get("question", "").strip()
    if not question:
        return {
            "statusCode": 400,
            "headers": {"Content-Type": "application/json"},
            "body": json.dumps({"error": "質問が空です"}, ensure_ascii=False),
        }

    logger.info(f"質問受信: {question[:50]}...")

    # ── S3 からドキュメント取得 ────────────────────────────
    document_text = ""
    source = "general"
    if S3_BUCKET_NAME:
        document_text = get_document_from_s3(S3_BUCKET_NAME, S3_PDF_KEY)
        if document_text:
            source = "s3_document"

    # ── Bedrock で回答生成 ────────────────────────────────
    answer = invoke_bedrock(document_text, question)

    logger.info(f"回答生成完了 (source={source}): {answer[:50]}...")

    return {
        "statusCode": 200,
        "headers": {
            "Content-Type": "application/json",
            "Access-Control-Allow-Origin": "*",
        },
        "body": json.dumps({"answer": answer, "source": source}, ensure_ascii=False),
    }
