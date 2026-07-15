import {
  getExtension,
  isSupportedExtension,
  decodeS3Key,
  buildSkippedResult,
  buildSuccessResult,
  buildErrorResult,
  parseSqsBody,
} from "./ingestion_client";

// ── getExtension ──────────────────────────────────────────
describe("getExtension", () => {
  it("should return .pdf for PDF file", () => {
    expect(getExtension("document.pdf")).toBe(".pdf");
  });

  it("should return .txt for text file", () => {
    expect(getExtension("knowledge.txt")).toBe(".txt");
  });

  it("should return .md for markdown file", () => {
    expect(getExtension("readme.md")).toBe(".md");
  });

  it("should return .docx for Word file", () => {
    expect(getExtension("policy.docx")).toBe(".docx");
  });

  it("should return lowercase extension for uppercase input", () => {
    expect(getExtension("FILE.PDF")).toBe(".pdf");
    expect(getExtension("FILE.TXT")).toBe(".txt");
  });

  it("should return empty string for file without extension", () => {
    expect(getExtension("README")).toBe("");
  });

  it("should handle paths with directories", () => {
    expect(getExtension("documents/policy/rules.pdf")).toBe(".pdf");
  });

  it("should handle dotfiles correctly", () => {
    expect(getExtension(".hidden")).toBe(".hidden");
  });
});

// ── isSupportedExtension ──────────────────────────────────
describe("isSupportedExtension", () => {
  it("should return true for .pdf", () => {
    expect(isSupportedExtension("document.pdf")).toBe(true);
  });

  it("should return true for .txt", () => {
    expect(isSupportedExtension("data.txt")).toBe(true);
  });

  it("should return true for .md", () => {
    expect(isSupportedExtension("guide.md")).toBe(true);
  });

  it("should return true for .docx", () => {
    expect(isSupportedExtension("report.docx")).toBe(true);
  });

  it("should return false for .png", () => {
    expect(isSupportedExtension("image.png")).toBe(false);
  });

  it("should return false for .jpg", () => {
    expect(isSupportedExtension("photo.jpg")).toBe(false);
  });

  it("should return false for file without extension", () => {
    expect(isSupportedExtension("README")).toBe(false);
  });
});

// ── decodeS3Key ───────────────────────────────────────────
describe("decodeS3Key", () => {
  it("should decode URL-encoded characters", () => {
    expect(decodeS3Key("documents%2Fpolicy.pdf")).toBe("documents/policy.pdf");
  });

  it("should replace + with space", () => {
    expect(decodeS3Key("my+document.pdf")).toBe("my document.pdf");
  });

  it("should leave plain keys unchanged", () => {
    expect(decodeS3Key("document.pdf")).toBe("document.pdf");
  });

  it("should decode Japanese characters", () => {
    const encoded = encodeURIComponent("社内規定.pdf");
    expect(decodeS3Key(encoded)).toBe("社内規定.pdf");
  });
});

// ── buildSkippedResult ────────────────────────────────────
describe("buildSkippedResult", () => {
  it("should build skipped result with unsupported extension reason", () => {
    const result = buildSkippedResult("image.png", ".png");
    expect(result.key).toBe("image.png");
    expect(result.status).toBe("skipped");
    expect(result.reason).toContain(".png");
  });

  it("should not include success fields", () => {
    const result = buildSkippedResult("image.png", ".png");
    expect(result.bucket).toBeUndefined();
    expect(result.size).toBeUndefined();
  });
});

// ── buildSuccessResult ────────────────────────────────────
describe("buildSuccessResult", () => {
  it("should build success result with all fields", () => {
    const result = buildSuccessResult("policy.pdf", "my-bucket", 1024, "application/pdf");
    expect(result.key).toBe("policy.pdf");
    expect(result.status).toBe("success");
    expect(result.bucket).toBe("my-bucket");
    expect(result.size).toBe(1024);
    expect(result.content_type).toBe("application/pdf");
  });

  it("should not include reason field", () => {
    const result = buildSuccessResult("doc.pdf", "bucket", 100, "application/pdf");
    expect(result.reason).toBeUndefined();
  });
});

// ── buildErrorResult ──────────────────────────────────────
describe("buildErrorResult", () => {
  it("should build error result with reason", () => {
    const result = buildErrorResult("document.pdf", "NoSuchKey");
    expect(result.key).toBe("document.pdf");
    expect(result.status).toBe("error");
    expect(result.reason).toBe("NoSuchKey");
  });

  it("should not include success fields", () => {
    const result = buildErrorResult("doc.pdf", "AccessDenied");
    expect(result.bucket).toBeUndefined();
    expect(result.size).toBeUndefined();
  });
});

// ── parseSqsBody ──────────────────────────────────────────
describe("parseSqsBody", () => {
  it("should parse JSON string body", () => {
    const s3Record = { s3: { bucket: { name: "b" }, object: { key: "f.pdf" } } };
    const body = JSON.stringify({ Records: [s3Record] });
    const result = parseSqsBody({ body });
    expect(result?.Records).toHaveLength(1);
  });

  it("should handle object body directly", () => {
    const s3Record = { s3: { bucket: { name: "b" }, object: { key: "f.pdf" } } };
    const result = parseSqsBody({ body: { Records: [s3Record] } });
    expect(result?.Records).toHaveLength(1);
  });

  it("should return null for invalid JSON", () => {
    const result = parseSqsBody({ body: "invalid {{{" });
    expect(result).toBeNull();
  });

  it("should handle missing body as empty object", () => {
    const result = parseSqsBody({});
    expect(result).toBeDefined();
  });
});

// ── 追加テスト（ingestion_client） ───────────────────────

describe("getExtension (追加)", () => {
  it("should return last extension for multiple dots", () => {
    expect(getExtension("file.backup.txt")).toBe(".txt");
  });

  it("should return empty string for empty input", () => {
    expect(getExtension("")).toBe("");
  });

  it("should handle S3-style path with extension", () => {
    expect(getExtension("prefix/subdir/document.pdf")).toBe(".pdf");
  });

  it("should normalize .MD to .md", () => {
    expect(getExtension("README.MD")).toBe(".md");
  });
});

describe("isSupportedExtension (追加)", () => {
  it("should return false for .csv", () => {
    expect(isSupportedExtension("data.csv")).toBe(false);
  });

  it("should return false for .zip", () => {
    expect(isSupportedExtension("archive.zip")).toBe(false);
  });

  it("should return true for .txt in S3-style path", () => {
    expect(isSupportedExtension("prefix/guide.txt")).toBe(true);
  });

  it("should return true for uppercase .PDF (normalized)", () => {
    expect(isSupportedExtension("policy.PDF")).toBe(true);
  });
});

describe("decodeS3Key (追加)", () => {
  it("should replace multiple + with spaces", () => {
    expect(decodeS3Key("hello+world+test.pdf")).toBe("hello world test.pdf");
  });

  it("should leave already-decoded key unchanged", () => {
    expect(decodeS3Key("plain-file.txt")).toBe("plain-file.txt");
  });

  it("should decode %20 as space", () => {
    expect(decodeS3Key("my%20document.pdf")).toBe("my document.pdf");
  });
});

describe("buildSkippedResult (追加)", () => {
  it("should contain 'unsupported extension' in reason", () => {
    const result = buildSkippedResult("photo.png", ".png");
    expect(result.reason).toContain("unsupported extension");
  });

  it("should have status 'skipped'", () => {
    expect(buildSkippedResult("x.csv", ".csv").status).toBe("skipped");
  });
});

describe("buildSuccessResult (追加)", () => {
  it("should preserve size=0", () => {
    const result = buildSuccessResult("empty.txt", "bucket", 0, "text/plain");
    expect(result.size).toBe(0);
  });

  it("should preserve content_type exactly", () => {
    const result = buildSuccessResult("doc.pdf", "bucket", 512, "application/pdf");
    expect(result.content_type).toBe("application/pdf");
  });
});

describe("buildErrorResult (追加)", () => {
  it("should preserve long reason string", () => {
    const long = "Error: ".repeat(20);
    const result = buildErrorResult("doc.pdf", long);
    expect(result.reason).toBe(long);
  });

  it("should have status 'error'", () => {
    expect(buildErrorResult("x.pdf", "reason").status).toBe("error");
  });
});

describe("parseSqsBody (追加)", () => {
  it("should return object even without Records key", () => {
    const result = parseSqsBody({ body: JSON.stringify({ other: "data" }) });
    expect(result).toBeDefined();
    expect(result).not.toBeNull();
  });

  it("should handle empty Records array", () => {
    const result = parseSqsBody({ body: JSON.stringify({ Records: [] }) });
    expect(result?.Records).toHaveLength(0);
  });
});
