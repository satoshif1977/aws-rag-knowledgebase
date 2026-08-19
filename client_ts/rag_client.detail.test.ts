import {
  isDocumentAvailable,
  truncateDocument,
  buildUserMessage,
  buildBedrockPayload,
  parseQuestion,
  buildSuccessResponse,
  buildErrorResponse,
} from "./rag_client";

// ── isDocumentAvailable: 境界値・Unicode ────────────────────
describe("isDocumentAvailable - boundary & unicode", () => {
  it("should return true for Japanese text", () => {
    expect(isDocumentAvailable("社内規定")).toBe(true);
  });

  it("should return true for mixed content with leading spaces", () => {
    expect(isDocumentAvailable("  hello  ")).toBe(true);
  });

  it("should return false for multiple newlines", () => {
    expect(isDocumentAvailable("\n\n\n")).toBe(false);
  });

  it("should return false for carriage return and newline", () => {
    expect(isDocumentAvailable("\r\n")).toBe(false);
  });

  it("should return true for zero followed by text", () => {
    expect(isDocumentAvailable("0")).toBe(true);
  });

  it("should return true for emoji content", () => {
    expect(isDocumentAvailable("🎉")).toBe(true);
  });

  it("should return false for form feed and vertical tab", () => {
    expect(isDocumentAvailable("\f\v")).toBe(false);
  });
});

// ── truncateDocument: 境界値・マルチバイト ───────────────────
describe("truncateDocument - boundary & multibyte", () => {
  it("should truncate exactly at boundary", () => {
    const text = "abcde";
    expect(truncateDocument(text, 3)).toBe("abc");
  });

  it("should handle maxChars=1", () => {
    expect(truncateDocument("hello", 1)).toBe("h");
  });

  it("should handle Japanese text truncation", () => {
    const text = "あいうえお";
    expect(truncateDocument(text, 3)).toBe("あいう");
  });

  it("should return full text when maxChars exceeds length", () => {
    expect(truncateDocument("short", 1000)).toBe("short");
  });

  it("should handle negative maxChars as empty string", () => {
    expect(truncateDocument("abc", -1)).toBe("");
  });

  it("should handle very large maxChars", () => {
    const text = "test";
    expect(truncateDocument(text, Number.MAX_SAFE_INTEGER)).toBe("test");
  });

  it("should preserve whitespace in truncated output", () => {
    expect(truncateDocument("a b c d e", 5)).toBe("a b c");
  });
});

// ── buildUserMessage: 統合テスト ───────────────────────────
describe("buildUserMessage - integration", () => {
  it("should contain document section marker with valid document", () => {
    const msg = buildUserMessage("内容", "質問");
    expect(msg).toMatch(/【社内ドキュメント】/);
  });

  it("should contain question section marker", () => {
    const msg = buildUserMessage("doc", "質問");
    expect(msg).toMatch(/【質問】/);
  });

  it("should not contain document section marker when empty", () => {
    const msg = buildUserMessage("", "質問");
    expect(msg).not.toMatch(/【社内ドキュメント】/);
  });

  it("should contain fallback instruction when no document", () => {
    const msg = buildUserMessage("", "質問");
    expect(msg).toContain("一般的な知識");
  });

  it("should handle special characters in question", () => {
    const msg = buildUserMessage("doc", "SELECT * FROM users;");
    expect(msg).toContain("SELECT * FROM users;");
  });

  it("should handle multiline document", () => {
    const doc = "行1\n行2\n行3";
    const msg = buildUserMessage(doc, "質問");
    expect(msg).toContain("行1\n行2\n行3");
  });

  it("should handle multiline question", () => {
    const msg = buildUserMessage("doc", "質問1\n質問2");
    expect(msg).toContain("質問1\n質問2");
  });

  it("should truncate document at 8000 chars boundary", () => {
    const doc = "x".repeat(8001);
    const msg = buildUserMessage(doc, "q");
    expect(msg).not.toContain("x".repeat(8001));
    expect(msg).toContain("x".repeat(8000));
  });
});

// ── buildBedrockPayload: 構造詳細テスト ─────────────────────
describe("buildBedrockPayload - structure detail", () => {
  it("should have exactly one message", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.messages).toHaveLength(1);
  });

  it("should include system prompt about company policies", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.system).toContain("社内規定");
    expect(payload.system).toContain("ポリシー");
  });

  it("should include confidentiality instruction in system prompt", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.system).toContain("個人情報");
    expect(payload.system).toContain("機密情報");
  });

  it("should include fallback instruction in system prompt", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.system).toContain("担当部署にご確認ください");
  });

  it("should pass document content through to message", () => {
    const payload = buildBedrockPayload("テスト文書の内容", "質問テキスト");
    expect(payload.messages[0].content).toContain("テスト文書の内容");
  });

  it("should pass question through to message", () => {
    const payload = buildBedrockPayload("doc", "有給休暇の日数は？");
    expect(payload.messages[0].content).toContain("有給休暇の日数は？");
  });

  it("should use bedrock API version", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.anthropic_version).toMatch(/^bedrock-\d{4}-\d{2}-\d{2}$/);
  });

  it("should have max_tokens as a positive number", () => {
    const payload = buildBedrockPayload("doc", "q");
    expect(payload.max_tokens).toBeGreaterThan(0);
  });
});

// ── parseQuestion: エッジケース ──────────────────────────────
describe("parseQuestion - edge cases", () => {
  it("should handle null body", () => {
    const event = { body: null };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should handle question with only spaces", () => {
    const event = { body: JSON.stringify({ question: "     " }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should handle very long question", () => {
    const longQ = "質問".repeat(5000);
    const event = { body: JSON.stringify({ question: longQ }) };
    expect(parseQuestion(event)).toBe(longQ);
  });

  it("should handle question with special characters", () => {
    const event = { body: JSON.stringify({ question: '<script>alert("xss")</script>' }) };
    expect(parseQuestion(event)).toBe('<script>alert("xss")</script>');
  });

  it("should handle question with newlines", () => {
    const event = { body: JSON.stringify({ question: "行1\n行2" }) };
    expect(parseQuestion(event)).toBe("行1\n行2");
  });

  it("should return null for boolean question field", () => {
    const event = { body: JSON.stringify({ question: true }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should return null for array question field", () => {
    const event = { body: JSON.stringify({ question: ["a", "b"] }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should return null for null question field", () => {
    const event = { body: JSON.stringify({ question: null }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should handle nested JSON body string", () => {
    const event = { body: JSON.stringify({ question: "valid", extra: { nested: true } }) };
    expect(parseQuestion(event)).toBe("valid");
  });

  it("should trim tabs from question", () => {
    const event = { body: JSON.stringify({ question: "\t質問\t" }) };
    expect(parseQuestion(event)).toBe("質問");
  });
});

// ── buildSuccessResponse: 詳細検証 ─────────────────────────
describe("buildSuccessResponse - detail", () => {
  it("should have statusCode exactly 200", () => {
    const res = buildSuccessResponse("answer", "s3_document");
    expect(res.statusCode).toBe(200);
  });

  it("should serialize answer with Japanese characters", () => {
    const res = buildSuccessResponse("日本語の回答です", "s3_document");
    const body = JSON.parse(res.body);
    expect(body.answer).toBe("日本語の回答です");
  });

  it("should include both required headers", () => {
    const res = buildSuccessResponse("a", "general");
    expect(res.headers).toHaveProperty("Content-Type");
    expect(res.headers).toHaveProperty("Access-Control-Allow-Origin");
  });

  it("should handle empty answer string", () => {
    const res = buildSuccessResponse("", "general");
    const body = JSON.parse(res.body);
    expect(body.answer).toBe("");
  });

  it("should preserve source as s3_document", () => {
    const res = buildSuccessResponse("a", "s3_document");
    const body = JSON.parse(res.body);
    expect(body.source).toBe("s3_document");
  });

  it("should produce parseable JSON body", () => {
    const res = buildSuccessResponse("回答", "general");
    const body = JSON.parse(res.body);
    expect(body).toHaveProperty("answer");
    expect(body).toHaveProperty("source");
  });
});

// ── buildErrorResponse: 詳細検証 ───────────────────────────
describe("buildErrorResponse - detail", () => {
  it("should serialize error message in body", () => {
    const res = buildErrorResponse(400, "Bad Request");
    const body = JSON.parse(res.body);
    expect(body.error).toBe("Bad Request");
  });

  it("should handle 503 Service Unavailable", () => {
    const res = buildErrorResponse(503, "Service Unavailable");
    expect(res.statusCode).toBe(503);
  });

  it("should handle empty error message", () => {
    const res = buildErrorResponse(500, "");
    const body = JSON.parse(res.body);
    expect(body.error).toBe("");
  });

  it("should handle Japanese error message", () => {
    const res = buildErrorResponse(400, "質問が入力されていません");
    const body = JSON.parse(res.body);
    expect(body.error).toBe("質問が入力されていません");
  });

  it("should include both required headers", () => {
    const res = buildErrorResponse(500, "err");
    expect(res.headers).toHaveProperty("Content-Type");
    expect(res.headers).toHaveProperty("Access-Control-Allow-Origin");
  });

  it("should produce parseable JSON body for any status", () => {
    const res = buildErrorResponse(429, "Too Many Requests");
    expect(() => JSON.parse(res.body)).not.toThrow();
  });
});
