import {
  isDocumentAvailable,
  truncateDocument,
  buildUserMessage,
  buildBedrockPayload,
  parseQuestion,
  buildSuccessResponse,
  buildErrorResponse,
} from "./rag_client";

// ── isDocumentAvailable ───────────────────────────────────
describe("isDocumentAvailable", () => {
  it("should return true for non-empty text", () => {
    expect(isDocumentAvailable("社内規定の内容")).toBe(true);
  });

  it("should return false for empty string", () => {
    expect(isDocumentAvailable("")).toBe(false);
  });

  it("should return false for whitespace-only string", () => {
    expect(isDocumentAvailable("   \n\t  ")).toBe(false);
  });
});

// ── truncateDocument ──────────────────────────────────────
describe("truncateDocument", () => {
  it("should return full text when under the limit", () => {
    const text = "短いテキスト";
    expect(truncateDocument(text)).toBe(text);
  });

  it("should truncate text to default 8000 chars", () => {
    const longText = "a".repeat(10000);
    const result = truncateDocument(longText);
    expect(result.length).toBe(8000);
  });

  it("should truncate to custom maxChars", () => {
    const text = "a".repeat(500);
    expect(truncateDocument(text, 100).length).toBe(100);
  });

  it("should return full text when exactly at limit", () => {
    const text = "a".repeat(8000);
    expect(truncateDocument(text)).toBe(text);
  });
});

// ── buildUserMessage ──────────────────────────────────────
describe("buildUserMessage", () => {
  it("should include document and question when document is available", () => {
    const msg = buildUserMessage("規定の内容", "有給休暇は何日ですか？");
    expect(msg).toContain("社内ドキュメント");
    expect(msg).toContain("規定の内容");
    expect(msg).toContain("有給休暇は何日ですか？");
  });

  it("should use fallback message when document is empty", () => {
    const msg = buildUserMessage("", "質問です");
    expect(msg).toContain("社内ドキュメントが見つかりませんでした");
    expect(msg).toContain("質問です");
    expect(msg).not.toContain("社内ドキュメント】");
  });

  it("should truncate long documents to 8000 chars", () => {
    const longDoc = "a".repeat(10000);
    const msg = buildUserMessage(longDoc, "質問");
    const docSection = msg.split("【質問】")[0];
    expect(docSection).not.toContain("a".repeat(10000));
    expect(msg).toContain("a".repeat(8000));
  });
});

// ── buildBedrockPayload ───────────────────────────────────
describe("buildBedrockPayload", () => {
  it("should build valid payload structure", () => {
    const payload = buildBedrockPayload("ドキュメント", "質問");
    expect(payload.anthropic_version).toBe("bedrock-2023-05-31");
    expect(payload.max_tokens).toBe(1000);
    expect(payload.messages).toHaveLength(1);
    expect(payload.messages[0].role).toBe("user");
  });

  it("should include system prompt", () => {
    const payload = buildBedrockPayload("doc", "question");
    expect(payload.system).toContain("社内規定");
  });

  it("should include document in message content when available", () => {
    const payload = buildBedrockPayload("規定内容", "質問テキスト");
    expect(payload.messages[0].content).toContain("規定内容");
    expect(payload.messages[0].content).toContain("質問テキスト");
  });

  it("should use fallback message when document is empty", () => {
    const payload = buildBedrockPayload("", "質問");
    expect(payload.messages[0].content).toContain("見つかりませんでした");
  });

  it("should use custom model ID when provided", () => {
    const payload = buildBedrockPayload("doc", "question", "custom-model");
    expect(payload).toBeDefined();
  });
});

// ── parseQuestion ─────────────────────────────────────────
describe("parseQuestion", () => {
  it("should extract question from JSON string body", () => {
    const event = { body: JSON.stringify({ question: "質問です" }) };
    expect(parseQuestion(event)).toBe("質問です");
  });

  it("should extract question from object body", () => {
    const event = { body: { question: "オブジェクト質問" } };
    expect(parseQuestion(event)).toBe("オブジェクト質問");
  });

  it("should return null for invalid JSON body", () => {
    const event = { body: "invalid json {{" };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should return null for empty question", () => {
    const event = { body: JSON.stringify({ question: "   " }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should return null for missing question field", () => {
    const event = { body: JSON.stringify({ other: "field" }) };
    expect(parseQuestion(event)).toBeNull();
  });

  it("should return null when body is missing", () => {
    const event = {};
    expect(parseQuestion(event)).toBeNull();
  });

  it("should trim whitespace from question", () => {
    const event = { body: JSON.stringify({ question: "  質問  " }) };
    expect(parseQuestion(event)).toBe("質問");
  });
});

// ── buildSuccessResponse ──────────────────────────────────
describe("buildSuccessResponse", () => {
  it("should return 200 with answer and source", () => {
    const response = buildSuccessResponse("回答テキスト", "s3_document");
    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body.answer).toBe("回答テキスト");
    expect(body.source).toBe("s3_document");
  });

  it("should include CORS header", () => {
    const response = buildSuccessResponse("回答", "general");
    expect(response.headers["Access-Control-Allow-Origin"]).toBe("*");
  });

  it("should work with general source", () => {
    const response = buildSuccessResponse("一般回答", "general");
    const body = JSON.parse(response.body);
    expect(body.source).toBe("general");
  });
});

// ── buildErrorResponse ────────────────────────────────────
describe("buildErrorResponse", () => {
  it("should return 400 with error message", () => {
    const response = buildErrorResponse(400, "質問が空です");
    expect(response.statusCode).toBe(400);
    const body = JSON.parse(response.body);
    expect(body.error).toBe("質問が空です");
  });

  it("should return custom status code", () => {
    const response = buildErrorResponse(500, "Internal Server Error");
    expect(response.statusCode).toBe(500);
  });

  it("should include Content-Type header", () => {
    const response = buildErrorResponse(400, "error");
    expect(response.headers["Content-Type"]).toBe("application/json");
  });
});
