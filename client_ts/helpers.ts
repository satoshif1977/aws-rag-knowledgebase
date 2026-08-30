/**
 * aws-rag-knowledgebase: 共通ヘルパー関数・定数
 *
 * rag_client.ts / ingestion_client.ts から抽出した
 * 汎用ユーティリティ関数群。ドメインに依存しない純粋関数で構成。
 */

import type { LambdaResponse } from "./types";

// ── 共通定数 ──────────────────────────────────────────────────

/** Lambda レスポンス共通ヘッダー */
export const DEFAULT_HEADERS: Record<string, string> = {
  "Content-Type": "application/json",
  "Access-Control-Allow-Origin": "*",
};

/** ドキュメント切り詰め上限（文字数） */
export const MAX_DOCUMENT_CHARS = 8000;

/** サポート対象拡張子（ingestion_handler.py と一致） */
export const SUPPORTED_EXTENSIONS = new Set([".pdf", ".txt", ".md", ".docx"]);

// ── JSON パースユーティリティ ──────────────────────────────────

/**
 * JSON 文字列またはオブジェクトを安全にパースする
 * rag_client.parseQuestion / ingestion_client.parseSqsBody の共通パターン統合
 */
export function safeJsonParse<T>(
  raw: string | Record<string, unknown> | null | undefined,
): T | null {
  if (raw == null) {
    return safeJsonParse<T>("{}");
  }
  if (typeof raw !== "string") {
    return raw as unknown as T;
  }
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

// ── ドキュメントユーティリティ ─────────────────────────────────

/** ドキュメントが有効（空白以外の文字を含む）か判定する */
export function isDocumentAvailable(text: string): boolean {
  return text.trim().length > 0;
}

/** ドキュメントを最大文字数に切り詰める */
export function truncateDocument(
  text: string,
  maxChars: number = MAX_DOCUMENT_CHARS,
): string {
  return text.substring(0, maxChars);
}

// ── Lambda レスポンスビルダー ──────────────────────────────────

/** 成功レスポンスを構築する */
export function buildSuccessResponse(
  answer: string,
  source: "s3_document" | "general",
): LambdaResponse {
  return {
    statusCode: 200,
    headers: { ...DEFAULT_HEADERS },
    body: JSON.stringify({ answer, source }),
  };
}

/** エラーレスポンスを構築する */
export function buildErrorResponse(
  statusCode: number,
  message: string,
): LambdaResponse {
  return {
    statusCode,
    headers: { ...DEFAULT_HEADERS },
    body: JSON.stringify({ error: message }),
  };
}

// ── S3 / ファイルユーティリティ ───────────────────────────────

/** ファイル名から拡張子を取得する（小文字化） */
export function getExtension(key: string): string {
  const lastDot = key.lastIndexOf(".");
  return lastDot !== -1 ? key.substring(lastDot).toLowerCase() : "";
}

/** 対応拡張子かチェックする */
export function isSupportedExtension(key: string): boolean {
  return SUPPORTED_EXTENSIONS.has(getExtension(key));
}

/** S3 キーを URL デコードする（+ → スペース変換含む） */
export function decodeS3Key(key: string): string {
  return decodeURIComponent(key.replace(/\+/g, " "));
}
