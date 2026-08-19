import {
  getExtension,
  isSupportedExtension,
  decodeS3Key,
  buildSkippedResult,
  buildSuccessResult,
  buildErrorResult,
  parseSqsBody,
} from "./ingestion_client";

// ── getExtension: 境界値・特殊パターン ──────────────────────
describe("getExtension - boundary & special patterns", () => {
  it("should return .html for HTML file", () => {
    expect(getExtension("index.html")).toBe(".html");
  });

  it("should return .csv for CSV file", () => {
    expect(getExtension("data.csv")).toBe(".csv");
  });

  it("should return .json for JSON file", () => {
    expect(getExtension("config.json")).toBe(".json");
  });

  it("should return last extension for triple dots", () => {
    expect(getExtension("archive.tar.gz")).toBe(".gz");
  });

  it("should handle filename with spaces", () => {
    expect(getExtension("my document.pdf")).toBe(".pdf");
  });

  it("should handle filename starting with dot and having extension", () => {
    expect(getExtension(".gitignore.bak")).toBe(".bak");
  });

  it("should handle deeply nested S3 path", () => {
    expect(getExtension("a/b/c/d/e/f/file.txt")).toBe(".txt");
  });

  it("should handle Japanese filename", () => {
    expect(getExtension("社内規定.pdf")).toBe(".pdf");
  });

  it("should return .xlsx for Excel file", () => {
    expect(getExtension("report.xlsx")).toBe(".xlsx");
  });

  it("should handle extension with numbers", () => {
    expect(getExtension("model.h5")).toBe(".h5");
  });
});

// ── isSupportedExtension: 網羅的テスト ──────────────────────
describe("isSupportedExtension - comprehensive", () => {
  it.each([
    ["document.pdf", true],
    ["readme.txt", true],
    ["guide.md", true],
    ["report.docx", true],
    ["image.png", false],
    ["photo.jpg", false],
    ["data.csv", false],
    ["archive.zip", false],
    ["script.py", false],
    ["style.css", false],
    ["index.html", false],
    ["config.json", false],
    ["video.mp4", false],
    ["README", false],
  ])("should return %s for '%s'", (filename, expected) => {
    expect(isSupportedExtension(filename)).toBe(expected);
  });

  it("should return true for .DOCX (case insensitive)", () => {
    expect(isSupportedExtension("report.DOCX")).toBe(true);
  });

  it("should return true for .Md (mixed case)", () => {
    expect(isSupportedExtension("guide.Md")).toBe(true);
  });

  it("should return true for .TXT in deep path", () => {
    expect(isSupportedExtension("folder/sub/doc.TXT")).toBe(true);
  });

  it("should return false for empty string", () => {
    expect(isSupportedExtension("")).toBe(false);
  });
});

// ── decodeS3Key: エッジケース ──────────────────────────────
describe("decodeS3Key - edge cases", () => {
  it("should decode mixed encoded and plain text", () => {
    expect(decodeS3Key("folder%2Fmy+doc.pdf")).toBe("folder/my doc.pdf");
  });

  it("should handle empty string", () => {
    expect(decodeS3Key("")).toBe("");
  });

  it("should handle double-encoded slash", () => {
    expect(decodeS3Key("%252F")).toBe("%2F");
  });

  it("should decode special characters", () => {
    expect(decodeS3Key("file%23name.pdf")).toBe("file#name.pdf");
  });

  it("should decode parentheses", () => {
    expect(decodeS3Key("file%28v2%29.pdf")).toBe("file(v2).pdf");
  });

  it("should handle consecutive plus signs", () => {
    expect(decodeS3Key("a++b+++c.pdf")).toBe("a  b   c.pdf");
  });

  it("should handle only plus signs", () => {
    expect(decodeS3Key("+++")).toBe("   ");
  });

  it("should decode at sign", () => {
    expect(decodeS3Key("user%40domain.txt")).toBe("user@domain.txt");
  });

  it("should decode ampersand", () => {
    expect(decodeS3Key("a%26b.pdf")).toBe("a&b.pdf");
  });

  it("should handle key with no encoding needed", () => {
    expect(decodeS3Key("simple-file_name.pdf")).toBe("simple-file_name.pdf");
  });
});

// ── buildSkippedResult: 構造検証 ────────────────────────────
describe("buildSkippedResult - structure", () => {
  it("should set key to input filename", () => {
    expect(buildSkippedResult("test.png", ".png").key).toBe("test.png");
  });

  it("should include extension in reason message", () => {
    const result = buildSkippedResult("data.csv", ".csv");
    expect(result.reason).toContain(".csv");
  });

  it("should always have status 'skipped'", () => {
    expect(buildSkippedResult("a.jpg", ".jpg").status).toBe("skipped");
    expect(buildSkippedResult("b.mp4", ".mp4").status).toBe("skipped");
  });

  it("should not have bucket field", () => {
    expect(buildSkippedResult("x.zip", ".zip").bucket).toBeUndefined();
  });

  it("should not have size field", () => {
    expect(buildSkippedResult("x.zip", ".zip").size).toBeUndefined();
  });

  it("should not have content_type field", () => {
    expect(buildSkippedResult("x.zip", ".zip").content_type).toBeUndefined();
  });
});

// ── buildSuccessResult: 構造検証 ────────────────────────────
describe("buildSuccessResult - structure", () => {
  it("should set all fields correctly", () => {
    const result = buildSuccessResult("doc.pdf", "my-bucket", 2048, "application/pdf");
    expect(result.key).toBe("doc.pdf");
    expect(result.status).toBe("success");
    expect(result.bucket).toBe("my-bucket");
    expect(result.size).toBe(2048);
    expect(result.content_type).toBe("application/pdf");
  });

  it("should handle very large file size", () => {
    const result = buildSuccessResult("big.pdf", "b", 5368709120, "application/pdf");
    expect(result.size).toBe(5368709120);
  });

  it("should handle text/plain content type", () => {
    const result = buildSuccessResult("doc.txt", "b", 100, "text/plain");
    expect(result.content_type).toBe("text/plain");
  });

  it("should handle text/markdown content type", () => {
    const result = buildSuccessResult("doc.md", "b", 50, "text/markdown");
    expect(result.content_type).toBe("text/markdown");
  });

  it("should not have reason field", () => {
    const result = buildSuccessResult("doc.pdf", "b", 100, "application/pdf");
    expect(result.reason).toBeUndefined();
  });

  it("should handle bucket name with dots", () => {
    const result = buildSuccessResult("doc.pdf", "my.bucket.name", 100, "application/pdf");
    expect(result.bucket).toBe("my.bucket.name");
  });

  it("should handle S3-style key with prefix", () => {
    const result = buildSuccessResult("prefix/sub/doc.pdf", "b", 100, "application/pdf");
    expect(result.key).toBe("prefix/sub/doc.pdf");
  });
});

// ── buildErrorResult: 構造検証 ──────────────────────────────
describe("buildErrorResult - structure", () => {
  it("should set key and reason correctly", () => {
    const result = buildErrorResult("doc.pdf", "AccessDenied");
    expect(result.key).toBe("doc.pdf");
    expect(result.reason).toBe("AccessDenied");
  });

  it("should always have status 'error'", () => {
    expect(buildErrorResult("a.pdf", "err1").status).toBe("error");
    expect(buildErrorResult("b.txt", "err2").status).toBe("error");
  });

  it("should not have bucket field", () => {
    expect(buildErrorResult("doc.pdf", "err").bucket).toBeUndefined();
  });

  it("should not have size field", () => {
    expect(buildErrorResult("doc.pdf", "err").size).toBeUndefined();
  });

  it("should not have content_type field", () => {
    expect(buildErrorResult("doc.pdf", "err").content_type).toBeUndefined();
  });

  it("should handle NoSuchKey error", () => {
    const result = buildErrorResult("missing.pdf", "NoSuchKey");
    expect(result.reason).toBe("NoSuchKey");
  });

  it("should handle InternalError", () => {
    const result = buildErrorResult("doc.pdf", "InternalError: Lambda timeout");
    expect(result.reason).toContain("InternalError");
  });

  it("should handle empty reason", () => {
    const result = buildErrorResult("doc.pdf", "");
    expect(result.reason).toBe("");
  });
});

// ── parseSqsBody: 複合パターン ─────────────────────────────
describe("parseSqsBody - complex patterns", () => {
  it("should parse single S3 record", () => {
    const record = {
      body: JSON.stringify({
        Records: [{ s3: { bucket: { name: "test-bucket" }, object: { key: "doc.pdf" } } }],
      }),
    };
    const result = parseSqsBody(record);
    expect(result?.Records[0].s3.bucket.name).toBe("test-bucket");
  });

  it("should parse multiple S3 records", () => {
    const records = [
      { s3: { bucket: { name: "b" }, object: { key: "a.pdf" } } },
      { s3: { bucket: { name: "b" }, object: { key: "b.txt" } } },
    ];
    const result = parseSqsBody({ body: JSON.stringify({ Records: records }) });
    expect(result?.Records).toHaveLength(2);
  });

  it("should return null for body with syntax error", () => {
    expect(parseSqsBody({ body: "{invalid" })).toBeNull();
  });

  it("should return null for truncated JSON", () => {
    expect(parseSqsBody({ body: '{"Records": [' })).toBeNull();
  });

  it("should handle body with extra fields", () => {
    const result = parseSqsBody({
      body: JSON.stringify({ Records: [], extra: "data", count: 0 }),
    });
    expect(result?.Records).toHaveLength(0);
  });

  it("should handle S3 record with size field", () => {
    const record = {
      body: JSON.stringify({
        Records: [{ s3: { bucket: { name: "b" }, object: { key: "doc.pdf", size: 1024 } } }],
      }),
    };
    const result = parseSqsBody(record);
    expect(result?.Records[0].s3.object.size).toBe(1024);
  });

  it("should handle object body with Records", () => {
    const result = parseSqsBody({
      body: { Records: [{ s3: { bucket: { name: "b" }, object: { key: "f.pdf" } } }] },
    });
    expect(result?.Records).toHaveLength(1);
  });

  it("should handle body as number (edge case)", () => {
    const result = parseSqsBody({ body: 42 as unknown as string });
    expect(result).toBeDefined();
  });
});
