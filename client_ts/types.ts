// ── API Gateway / Lambda 共通 ─────────────────────────────
export interface LambdaResponse {
  statusCode: number;
  headers: Record<string, string>;
  body: string;
}

// ── RAG Q&A クライアント ───────────────────────────────────
export interface ApiGatewayEvent {
  body?: string | Record<string, unknown> | null;
}

export interface RagRequestBody {
  question: string;
}

export interface RagResponseBody {
  answer: string;
  source: "s3_document" | "general";
}

export interface BedrockMessage {
  role: "user";
  content: string;
}

export interface BedrockPayload {
  anthropic_version: string;
  max_tokens: number;
  system: string;
  messages: BedrockMessage[];
}

// ── ドキュメント取り込みクライアント ─────────────────────────
export interface S3Record {
  s3: {
    bucket: { name: string };
    object: { key: string; size?: number };
  };
}

export interface S3EventBody {
  Records: S3Record[];
}

export interface SqsRecord {
  messageId?: string;
  body?: string | Record<string, unknown>;
}

export type IngestionStatus = "success" | "skipped" | "error";

export interface IngestionResult {
  key: string;
  status: IngestionStatus;
  bucket?: string;
  size?: number;
  content_type?: string;
  reason?: string;
}

export interface IngestionHandlerResult {
  processed: IngestionResult[];
  skipped: IngestionResult[];
  errors: IngestionResult[];
  total: number;
}
