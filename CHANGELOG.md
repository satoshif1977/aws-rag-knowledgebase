# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [1.1.0] - 2026-05-19

### Added
- CONTRIBUTING.md 追加（PR プロセス・スタイルガイド）

### Changed
- Claude 3 Haiku → Claude 3.5 Haiku（`anthropic.claude-3-5-haiku-20241022-v1:0`）に移行（EOL: 2026-09-10）

## [1.0.1] - 2026-05-13

### Added
- SECURITY.md 追加
- README にトラブルシューティング・ローカル開発テスト・FAQ データ管理・RAG 精度向上方法セクション追加
- `.gitignore` に `.ruff_cache` / `.pytest_cache` 追加

### Fixed
- Lambda の f-string 内にコメント（`# コメント`）が混入していたバグを修正
  → Claude のプロンプトに不要なコメントが送信されていた問題を解消
- エラーレスポンス（400/500）に CORS ヘッダー `Access-Control-Allow-Origin: *` が欠落していた問題を修正

## [1.0.0] - 2026-03-12

### Added
- 初回実装：S3 + Lambda + API Gateway + Amazon Bedrock（Claude 3 Haiku）による RAG PoC
  - `GET /query` エンドポイントで S3 ドキュメントをコンテキストとした Q&A 回答
  - Streamlit Web UI（boto3 経由で Lambda を直接 Invoke）
- Terraform IaC（S3 / Lambda / API Gateway / IAM / CloudWatch Logs）
- GitHub Actions CI（Python lint + Checkov セキュリティスキャン）
- draw.io アーキテクチャ構成図
