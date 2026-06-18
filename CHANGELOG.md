# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [1.7.0] - 2026-06-18

### Added
- README に Go Test CI バッジ・Go 1.22 言語バッジを追加

## [1.6.0] - 2026-06-16

### Changed
- boto3 >=1.43.14 -> >=1.43.30
- actions/setup-go v5 -> v6
- actions/checkout v4 -> v6
- codecov/codecov-action v6 -> v7
- streamlit >=1.57.0 -> >=1.58.0

## [1.5.0] - 2026-06-04

### Added
- **EventBridge Pipes 追加**（`terraform/pipes.tf`）
  - SQS（document-ingestion キュー + DLQ）→ Pipes（拡張子フィルター）→ Lambda のパイプラインを Terraform で実装
  - `aws_pipes_pipe`: `.pdf` / `.txt` / `.md` / `.docx` のみ通過する suffix フィルターを設定（非対応ファイルは Lambda を起動せずに破棄）
  - `aws_s3_bucket_notification`: S3 ObjectCreated イベントを SQS へ自動送信
  - SQS: SSE 有効・DLQ 付き（3回失敗で DLQ へ移動）
  - IAM: Pipes 用・取り込み Lambda 用をそれぞれ最小権限で作成
- **ドキュメント取り込み Lambda**（`lambda/ingestion_handler.py`）
  - Pipes から SQS メッセージ（S3 イベント通知）を受け取り S3 からドキュメントを取得・検証
  - URL エンコードされたキーを自動デコード（日本語ファイル名対応）
  - 拡張子の二重チェック（Pipes フィルターをすり抜けたファイルへの保険）
  - レスポンスを `processed` / `skipped` / `errors` に分類して返却
  - 実運用拡張ポイント: Knowledge Base StartIngestionJob・DynamoDB 登録・SNS 通知
- **ユニットテスト追加**（`lambda/test_ingestion_handler.py`・11件、全体 21件）
  - PDF/TXT 取り込み成功・URL エンコードデコード・拡張子スキップ・S3 エラー・不正 JSON・dict 形式イベント
- **outputs 追加**: `ingestion_queue_url` / `ingestion_dlq_url` / `ingestion_pipe_name` / `ingestion_lambda_name`
- **`.gitignore` 更新**: `lambda_ingestion.zip` を追加

## [1.3.0] - 2026-06-01

### Changed
- Bedrock モデルを `Claude 3.5 Haiku`（ap-northeast-1 で使用不可）→ `Claude Haiku 4.5` に更新
  （`jp.anthropic.claude-haiku-4-5-20251001-v1:0`）
- IAM ポリシーに inference-profile ARN と全リージョン foundation-model ARN を追加

## [1.2.0] - 2026-05-29

### Changed
- デフォルトブランチを `master` → `main` に統一・`master` ブランチ削除
- CI ワークフローのブランチ指定を `main` のみに修正
- Dependabot: `hashicorp/aws` v5→v6（`terraform plan 0c/0d` 確認済み）・`streamlit` >=1.57.0・`boto3` >=1.43.14 を更新
- Dependabot: `actions/checkout` v6・`actions/setup-python` v6・`codecov/codecov-action` v6 を更新

### Removed
- 旧版アーキテクチャ構成図（`docs/architecture.drawio`・`docs/rag-architecture-v2.drawio`・`docs/rag-architecture-v2.drawio.png`）を削除
- README のディレクトリ構成を最新版（`rag-architecture-aws2026.drawio`）に更新

## [1.1.1] - 2026-05-26

### Fixed
- README のモデル名を `Claude 3 Haiku` → `Claude 3.5 Haiku` に統一（v1.1.0 でコード移行済みだったが README が未更新だった）

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
