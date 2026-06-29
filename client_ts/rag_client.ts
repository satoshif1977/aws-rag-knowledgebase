import type { ApiGatewayEvent, BedrockPayload, LambdaResponse } from "./types";

// ── 定数 ──────────────────────────────────────────────────
const DEFAULT_MODEL_ID = "jp.anthropic.claude-haiku-4-5-20251001-v1:0";
const MAX_DOCUMENT_CHARS = 8000;
const SYSTEM_PROMPT = `あなたは社内規定・ポリシーの専門アシスタントです。
提供された社内ドキュメントの内容に基づいて、質問に正確・簡潔に回答してください。
ドキュメントに記載がない場合は「この内容はドキュメントに記載がありません。担当部署にご確認ください。」と答えてください。
個人情報や機密情報には触れないでください。`;

const DEFAULT_HEADERS: Record<string, string> = {
  "Content-Type": "application/json",
  "Access-Control-Allow-Origin": "*",
};

// ── ドキュメントの有効判定 ─────────────────────────────────
export function isDocumentAvailable(text: string): boolean {
  return text.trim().length > 0;
}

// ── ドキュメントを最大文字数に切り詰める ──────────────────────
export function truncateDocument(text: string, maxChars: number = MAX_DOCUMENT_CHARS): string {
  return text.substring(0, maxChars);
}

// ── Bedrock 用ユーザーメッセージ構築 ───────────────────────
export function buildUserMessage(documentText: string, question: string): string {
  if (isDocumentAvailable(documentText)) {
    const truncated = truncateDocument(documentText);
    return `以下の社内ドキュメントを参照して質問に答えてください。

【社内ドキュメント】
${truncated}

【質問】
${question}`;
  }

  return `社内ドキュメントが見つかりませんでした。
一般的な知識で以下の質問に回答してください。

【質問】
${question}`;
}

// ── Bedrock リクエストボディ構築 ───────────────────────────
export function buildBedrockPayload(
  documentText: string,
  question: string,
  modelId: string = DEFAULT_MODEL_ID,
): BedrockPayload {
  return {
    anthropic_version: "bedrock-2023-05-31",
    max_tokens: 1000,
    system: SYSTEM_PROMPT,
    messages: [
      {
        role: "user",
        content: buildUserMessage(documentText, question),
      },
    ],
  };
}

// ── API Gateway イベントから質問を抽出 ─────────────────────
export function parseQuestion(event: ApiGatewayEvent): string | null {
  const bodyRaw = event.body ?? "{}";
  let body: Record<string, unknown>;

  try {
    body = typeof bodyRaw === "string" ? JSON.parse(bodyRaw) : bodyRaw;
  } catch {
    return null;
  }

  const question = typeof body["question"] === "string" ? body["question"].trim() : "";
  return question.length > 0 ? question : null;
}

// ── レスポンス構築ヘルパー ─────────────────────────────────
export function buildSuccessResponse(answer: string, source: "s3_document" | "general"): LambdaResponse {
  return {
    statusCode: 200,
    headers: { ...DEFAULT_HEADERS },
    body: JSON.stringify({ answer, source }),
  };
}

export function buildErrorResponse(statusCode: number, message: string): LambdaResponse {
  return {
    statusCode,
    headers: { ...DEFAULT_HEADERS },
    body: JSON.stringify({ error: message }),
  };
}
