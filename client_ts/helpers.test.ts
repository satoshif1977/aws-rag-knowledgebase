import {
  DEFAULT_HEADERS,
  MAX_DOCUMENT_CHARS,
  SUPPORTED_EXTENSIONS,
  safeJsonParse,
  isDocumentAvailable,
  truncateDocument,
  buildSuccessResponse,
  buildErrorResponse,
  getExtension,
  isSupportedExtension,
  decodeS3Key,
} from "./helpers";

// ── 定数エクスポート確認 ──────────────────────────────────────

describe("constants", () => {
  it("should export DEFAULT_HEADERS with Content-Type and CORS", () => {
    expect(DEFAULT_HEADERS["Content-Type"]).toBe("application/json");
    expect(DEFAULT_HEADERS["Access-Control-Allow-Origin"]).toBe("*");
  });

  it("should export MAX_DOCUMENT_CHARS as 8000", () => {
    expect(MAX_DOCUMENT_CHARS).toBe(8000);
  });

  it("should export SUPPORTED_EXTENSIONS with 4 types", () => {
    expect(SUPPORTED_EXTENSIONS.size).toBe(4);
    expect(SUPPORTED_EXTENSIONS.has(".pdf")).toBe(true);
    expect(SUPPORTED_EXTENSIONS.has(".txt")).toBe(true);
    expect(SUPPORTED_EXTENSIONS.has(".md")).toBe(true);
    expect(SUPPORTED_EXTENSIONS.has(".docx")).toBe(true);
  });

  it("should not include unsupported extensions", () => {
    expect(SUPPORTED_EXTENSIONS.has(".png")).toBe(false);
    expect(SUPPORTED_EXTENSIONS.has(".csv")).toBe(false);
    expect(SUPPORTED_EXTENSIONS.has(".json")).toBe(false);
  });
});

// ── safeJsonParse ─────────────────────────────────────────────

describe("safeJsonParse", () => {
  it("should parse valid JSON string", () => {
    const result = safeJsonParse<{ key: string }>('{"key": "value"}');
    expect(result).toEqual({ key: "value" });
  });

  it("should return null for invalid JSON string", () => {
    expect(safeJsonParse("{invalid")).toBeNull();
  });

  it("should return null for truncated JSON", () => {
    expect(safeJsonParse('{"key":')).toBeNull();
  });

  it("should return object directly when input is object", () => {
    const obj = { question: "テスト" };
    const result = safeJsonParse<typeof obj>(obj);
    expect(result).toBe(obj);
  });

  it("should parse empty JSON object string", () => {
    const result = safeJsonParse<Record<string, unknown>>("{}");
    expect(result).toEqual({});
  });

  it("should handle null input as empty object", () => {
    const result = safeJsonParse<Record<string, unknown>>(null);
    expect(result).toEqual({});
  });

  it("should handle undefined input as empty object", () => {
    const result = safeJsonParse<Record<string, unknown>>(undefined);
    expect(result).toEqual({});
  });

  it("should parse JSON array string", () => {
    const result = safeJsonParse<string[]>('["a", "b"]');
    expect(result).toEqual(["a", "b"]);
  });

  it("should parse nested JSON", () => {
    const result = safeJsonParse<{ a: { b: number } }>('{"a": {"b": 42}}');
    expect(result?.a.b).toBe(42);
  });

  it("should handle JSON with Japanese characters", () => {
    const result = safeJsonParse<{ q: string }>('{"q": "質問です"}');
    expect(result?.q).toBe("質問です");
  });

  it("should return null for empty string", () => {
    expect(safeJsonParse("")).toBeNull();
  });

  it("should handle JSON with numeric values", () => {
    const result = safeJsonParse<{ count: number }>('{"count": 100}');
    expect(result?.count).toBe(100);
  });

  it("should handle JSON with boolean values", () => {
    const result = safeJsonParse<{ flag: boolean }>('{"flag": true}');
    expect(result?.flag).toBe(true);
  });

  it("should handle JSON with null value", () => {
    const result = safeJsonParse<{ val: null }>('{"val": null}');
    expect(result?.val).toBeNull();
  });
});

// ── isDocumentAvailable（helpers 直接インポート） ──────────────

describe("isDocumentAvailable (from helpers)", () => {
  it("should return true for non-empty text", () => {
    expect(isDocumentAvailable("社内規定")).toBe(true);
  });

  it("should return false for empty string", () => {
    expect(isDocumentAvailable("")).toBe(false);
  });

  it("should return false for whitespace-only", () => {
    expect(isDocumentAvailable("  \t\n  ")).toBe(false);
  });
});

// ── truncateDocument（helpers 直接インポート） ─────────────────

describe("truncateDocument (from helpers)", () => {
  it("should return full text when under limit", () => {
    expect(truncateDocument("短い")).toBe("短い");
  });

  it("should truncate to default 8000 chars", () => {
    expect(truncateDocument("a".repeat(10000)).length).toBe(8000);
  });

  it("should truncate to custom maxChars", () => {
    expect(truncateDocument("abcdef", 3)).toBe("abc");
  });
});

// ── buildSuccessResponse（helpers 直接インポート） ─────────────

describe("buildSuccessResponse (from helpers)", () => {
  it("should return 200 with answer and source", () => {
    const res = buildSuccessResponse("回答", "s3_document");
    expect(res.statusCode).toBe(200);
    const body = JSON.parse(res.body);
    expect(body.answer).toBe("回答");
    expect(body.source).toBe("s3_document");
  });

  it("should include CORS and Content-Type headers", () => {
    const res = buildSuccessResponse("a", "general");
    expect(res.headers["Content-Type"]).toBe("application/json");
    expect(res.headers["Access-Control-Allow-Origin"]).toBe("*");
  });
});

// ── buildErrorResponse（helpers 直接インポート） ───────────────

describe("buildErrorResponse (from helpers)", () => {
  it("should return error with status code and message", () => {
    const res = buildErrorResponse(400, "Bad Request");
    expect(res.statusCode).toBe(400);
    const body = JSON.parse(res.body);
    expect(body.error).toBe("Bad Request");
  });

  it("should include CORS and Content-Type headers", () => {
    const res = buildErrorResponse(500, "err");
    expect(res.headers["Content-Type"]).toBe("application/json");
    expect(res.headers["Access-Control-Allow-Origin"]).toBe("*");
  });
});

// ── getExtension（helpers 直接インポート） ─────────────────────

describe("getExtension (from helpers)", () => {
  it("should return .pdf for PDF file", () => {
    expect(getExtension("document.pdf")).toBe(".pdf");
  });

  it("should return lowercase for uppercase input", () => {
    expect(getExtension("FILE.PDF")).toBe(".pdf");
  });

  it("should return empty for no extension", () => {
    expect(getExtension("README")).toBe("");
  });
});

// ── isSupportedExtension（helpers 直接インポート） ─────────────

describe("isSupportedExtension (from helpers)", () => {
  it("should return true for .pdf/.txt/.md/.docx", () => {
    expect(isSupportedExtension("a.pdf")).toBe(true);
    expect(isSupportedExtension("b.txt")).toBe(true);
    expect(isSupportedExtension("c.md")).toBe(true);
    expect(isSupportedExtension("d.docx")).toBe(true);
  });

  it("should return false for unsupported types", () => {
    expect(isSupportedExtension("e.png")).toBe(false);
    expect(isSupportedExtension("f.csv")).toBe(false);
  });
});

// ── decodeS3Key（helpers 直接インポート） ──────────────────────

describe("decodeS3Key (from helpers)", () => {
  it("should decode URL-encoded characters", () => {
    expect(decodeS3Key("a%2Fb.pdf")).toBe("a/b.pdf");
  });

  it("should replace + with space", () => {
    expect(decodeS3Key("my+doc.pdf")).toBe("my doc.pdf");
  });

  it("should leave plain keys unchanged", () => {
    expect(decodeS3Key("simple.pdf")).toBe("simple.pdf");
  });
});
