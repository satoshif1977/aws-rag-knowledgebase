import type { IngestionResult, SqsRecord, S3Record } from "./types";
import {
  getExtension,
  isSupportedExtension,
  decodeS3Key,
  safeJsonParse,
} from "./helpers";

// ── re-export（テスト互換） ─────────────────────────────────
export { getExtension, isSupportedExtension, decodeS3Key };

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
  return safeJsonParse<{ Records: S3Record[] }>(record.body);
}
