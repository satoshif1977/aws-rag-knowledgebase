import type { IngestionResult, SqsRecord, S3Record } from "./types";

// ── サポート対象拡張子（ingestion_handler.py と一致） ────────
const SUPPORTED_EXTENSIONS = new Set([".pdf", ".txt", ".md", ".docx"]);

// ── 拡張子取得 ─────────────────────────────────────────────
export function getExtension(key: string): string {
  const lastDot = key.lastIndexOf(".");
  return lastDot !== -1 ? key.substring(lastDot).toLowerCase() : "";
}

// ── 対応拡張子チェック ─────────────────────────────────────
export function isSupportedExtension(key: string): boolean {
  return SUPPORTED_EXTENSIONS.has(getExtension(key));
}

// ── S3 キーの URL デコード ─────────────────────────────────
export function decodeS3Key(key: string): string {
  return decodeURIComponent(key.replace(/\+/g, " "));
}

// ── 結果オブジェクト構築 ───────────────────────────────────
export function buildSkippedResult(key: string, ext: string): IngestionResult {
  return {
    key,
    status: "skipped",
    reason: `unsupported extension: ${ext}`,
  };
}

export function buildSuccessResult(
  key: string,
  bucket: string,
  size: number,
  contentType: string,
): IngestionResult {
  return {
    key,
    status: "success",
    bucket,
    size,
    content_type: contentType,
  };
}

export function buildErrorResult(key: string, reason: string): IngestionResult {
  return {
    key,
    status: "error",
    reason,
  };
}

// ── SQS レコードの body をパース ─────────────────────────────
export function parseSqsBody(record: SqsRecord): { Records: S3Record[] } | null {
  const bodyRaw = record.body ?? "{}";
  try {
    const parsed = typeof bodyRaw === "string" ? JSON.parse(bodyRaw) : bodyRaw;
    return parsed as { Records: S3Record[] };
  } catch {
    return null;
  }
}
