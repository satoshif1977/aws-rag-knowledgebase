# aws-rag-knowledgebase

![CI](https://github.com/satoshif1977/aws-rag-knowledgebase/actions/workflows/python-lint.yml/badge.svg)
![AWS](https://img.shields.io/badge/AWS-232F3E?style=flat&logo=amazon-aws&logoColor=white)
![Python](https://img.shields.io/badge/Python-3776AB?style=flat&logo=python&logoColor=white)
![Terraform](https://img.shields.io/badge/Terraform-623CE4?style=flat&logo=terraform&logoColor=white)
![Claude Code](https://img.shields.io/badge/Built%20with-Claude%20Code-orange?logo=anthropic)
![Claude Cowork](https://img.shields.io/badge/Daily%20Use-Claude%20Cowork-blueviolet?logo=anthropic)
![Claude Skills](https://img.shields.io/badge/Custom-Skills%20Configured-green?logo=anthropic)

社内ドキュメント（テキスト）を S3 に格納し、Amazon Bedrock（Claude）で Q&A 回答を返す **RAG PoC** です。
Serverless 構成（Lambda + API Gateway）で、Streamlit の Web UI から手軽に試せます。

---

## アーキテクチャ

![構成図](docs/rag-architecture-aws2026.drawio.png)

| # | 処理フロー |
|---|-----------|
| ① | Streamlit UI から Lambda を boto3 で直接 Invoke |
| ② | Lambda が S3 からドキュメントテキストを取得 |
| ③ | ドキュメント内容をコンテキストとして Bedrock（Claude 3.5 Haiku）に送信 |
| ④ | Bedrock が RAG 回答を生成して返却 |
| ⑤ | Streamlit が回答と参照元ソースをチャット形式で表示 |

---

## デモ

社内規定ドキュメントへの質問 → RAG 検索 → Bedrock 回答の流れ（質問3問）。

![RAG デモ](docs/demo/demo.gif)

---

## 使用技術

| カテゴリ | 技術 |
|---------|------|
| AI モデル | Amazon Bedrock（Claude 3.5 Haiku） |
| バックエンド | AWS Lambda（Python 3.11） |
| API | Amazon API Gateway（REST API） |
| ストレージ | Amazon S3 |
| 監視 | Amazon CloudWatch Logs |
| Web UI | Streamlit + boto3 |
| IaC | Terraform |

---

## スクリーンショット

### Streamlit デモ画面（社内規定ドキュメント Q&A）

![Streamlit デモ](docs/screenshots/02_streamlit_demo.png)

- 有給休暇・リモートワーク・経費申請について S3 ドキュメントから正確に回答
- `📂 S3 ドキュメント参照` ラベルでドキュメントベースの回答であることを明示

### Lambda 関数

| 関数概要 + API Gateway トリガー | コードソース・ランタイム |
|--------------------------------|------------------------|
| ![](docs/screenshots/05_lambda_trigger.png) | ![](docs/screenshots/01_lambda_code.png) |

### S3 バケット・ドキュメント

| バケット一覧 | knowledge.txt の詳細 |
|------------|---------------------|
| ![](docs/screenshots/06_s3_bucket.png) | ![](docs/screenshots/03_s3_document.png) |

### API Gateway

![API Gateway リソース](docs/screenshots/04_apigateway_resource.png)

---

## ディレクトリ構成

```
aws-rag-knowledgebase/
├── app/
│   ├── app.py              # Streamlit Web UI
│   └── requirements.txt
├── docs/
│   ├── rag-architecture-aws2026.drawio
│   ├── rag-architecture-aws2026.drawio.png
│   └── screenshots/        # 動作確認スクリーンショット
├── lambda/
│   └── index.py            # RAG 処理（S3 取得 + Bedrock 呼び出し）
└── terraform/
    ├── main.tf             # S3 / Lambda / API Gateway / IAM / CloudWatch
    ├── variables.tf
    ├── outputs.tf
    └── terraform.tfvars.example
```

---

## セットアップ

### 前提

- AWS CLI / aws-vault 設定済み
- Terraform >= 1.5
- Amazon Bedrock で Claude 3.5 Haiku のモデルアクセスを有効化済み

### デプロイ手順

```bash
# 1. Terraform 変数ファイルを作成
cd terraform
cp terraform.tfvars.example terraform.tfvars
# terraform.tfvars を環境に合わせて編集

# 2. デプロイ
aws-vault exec <profile> -- terraform init
aws-vault exec <profile> -- terraform apply

# 3. S3 にドキュメントをアップロード
aws-vault exec <profile> -- aws s3 cp ../knowledge.txt \
  s3://<バケット名>/documents/knowledge.txt

# 4. Streamlit 起動
cd ../app
aws-vault exec <profile> -- streamlit run app.py
```

### Streamlit サイドバー設定

| 項目 | 説明 |
|------|------|
| Lambda 関数名 | Terraform output の `lambda_function_name` |
| AWS リージョン | `ap-northeast-1`（デフォルト） |

---

## 技術的なポイント・工夫

- **Serverless RAG 実装**: OpenSearch 等の Vector DB を使わず、S3 テキスト＋プロンプトエンジニアリングで RAG を実現（PoC に最適な軽量構成）
- **API Gateway vs Lambda Function URL**: SCP によるブロックがある企業アカウントでは API Gateway が安定
- **IAM 最小権限**: S3・Bedrock・SSM のみに絞ったインラインポリシー
- **Lambda Function URL との差別化**: 本プロジェクトは REST API として `/query` エンドポイントを公開（aws-bedrock-agent との設計比較が可能）
- **コスト感**: Lambda（呼び出し課金）+ API Gateway（呼び出し課金）+ S3（保存料）≒ ほぼ無料（PoC 規模）

---

## FAQ データ管理・RAG 精度向上方法

### S3 ドキュメントの更新手順

```bash
# knowledge.txt を更新してから再アップロード
aws-vault exec <profile> -- aws s3 cp knowledge.txt \
  s3://<バケット名>/documents/knowledge.txt

# アップロード確認
aws-vault exec <profile> -- aws s3 ls \
  s3://<バケット名>/documents/
```

### ドキュメント作成のポイント

| 項目 | 推奨スタイル |
|---|---|
| 見出し | 「## 有給休暇申請」のような明確なセクション区切り |
| 回答形式 | 箇条書きで手順を列挙すると LLM が回答しやすい |
| キーワード | 質問で使われやすい言葉（「申請」「締め日」「ルール」）を必ず含める |
| ファイル分割 | 業務カテゴリ別に複数 .txt に分割してフォルダ管理が推奨 |

### RAG 精度向上のチューニング

| 方法 | 設定箇所 | 効果 |
|---|---|---|
| プロンプト調整 | `lambda/index.py` の `prompt` 変数 | 回答のトーン・形式を変更 |
| ドキュメント整備 | `knowledge.txt` の記述スタイル | 情報が密なほど回答精度が上がる |
| `max_tokens` 増加 | `lambda/index.py` の `max_tokens` | 長文回答が必要な場合に増やす |
| ドキュメント分割 | S3 に複数ファイルを配置 | カテゴリ別に Lambda で呼び分け可能 |

> **次のステップ**: 本格的な RAG には [aws-bedrock-knowledgebase-rag](https://github.com/satoshif1977/aws-bedrock-knowledgebase-rag) で Bedrock Knowledge Bases（OpenSearch Serverless）による Vector 検索を参照。

---

## 関連リポジトリ

| リポジトリ | 概要 |
|-----------|------|
| [aws-bedrock-agent](https://github.com/satoshif1977/aws-bedrock-agent) | Slack Bot + Bedrock Agent（Function URL 使用） |
| [terraform-3tier-webapp](https://github.com/satoshif1977/terraform-3tier-webapp) | 3 層 Web アーキテクチャ（VPC / ALB / EC2 / RDS） |

---

## 学習で気づいたこと・躓いたポイント

### Bedrock / Lambda

- **Bedrock モデルアクセスの有効化が必要**: コンソールから事前にモデルアクセスを申請しないと `AccessDeniedException` で詰まる。Terraform apply 前に必ず確認。
- **Lambda コールドスタートと API Gateway タイムアウト**: Lambda 初回起動（コールドスタート）が遅い場合、API Gateway の統合タイムアウト（デフォルト 29 秒）で先に切れることがある。PoC 規模ではほぼ問題ないが本番では Provisioned Concurrency が選択肢に入る。

### Terraform / S3

- **S3 バージョニング有効バケットの `terraform destroy` が失敗する**: バージョニングを有効にすると削除マーカーが残り、`terraform destroy` が途中で失敗する。バージョン一覧を取得して手動削除してから `destroy` する手順が必要。
- **API Gateway vs Lambda Function URL**: 企業アカウントでは SCP（サービスコントロールポリシー）により Lambda Function URL がブロックされていることがある。その場合は API Gateway が確実。

### Streamlit + aws-vault

- **Streamlit の起動コマンド**: `aws-vault exec <profile> -- streamlit run app.py` と aws-vault を前置するのがポイント。`streamlit run` だけでは IAM クレデンシャルが引き継がれない。

---

*Managed by Terraform / Powered by Amazon Bedrock*

---

## トラブルシューティング

| 症状 | 原因 | 対処法 |
|---|---|---|
| `AccessDeniedException` on Bedrock | モデルアクセスが未許可 | コンソール → Bedrock → モデルアクセスで Claude 3.5 Haiku を有効化 |
| Lambda が `503` を返す | コールドスタートが API Gateway タイムアウト（29秒）を超えた | Lambda メモリを増やすか Provisioned Concurrency を設定 |
| `terraform destroy` が失敗 | S3 バージョニング有効バケットに削除マーカーが残存 | `aws s3api list-object-versions` で確認 → 手動削除 → `destroy` を再実行 |
| Streamlit で `NoCredentialsError` | aws-vault を経由していない | `aws-vault exec <profile> -- streamlit run app.py` で起動 |
| Lambda 関数名が見つからない | Terraform outputs を未確認 | `terraform output lambda_function_name` で確認してサイドバーに入力 |

---

## ローカル開発・テスト方法

### Streamlit Web UI のローカル起動

```bash
cd app
pip install -r requirements.txt
aws-vault exec personal-dev-source -- streamlit run app.py
# http://localhost:8501 → サイドバーに Lambda 関数名を入力
```

### Python ユニットテスト（AWS 接続不要）

Lambda 関数のロジックを boto3 モックで検証します。

```bash
pip install pytest boto3 botocore
pytest lambda/test_index.py lambda/test_ingestion_handler.py -v
```

| テストファイル | テスト数 | 主な検証内容 |
|---|---|---|
| `lambda/test_index.py` | 10 件 | RAG クエリ・Bedrock 呼び出し・エラーハンドリング |
| `lambda/test_ingestion_handler.py` | 11 件 | S3 取り込み・Knowledge Base 同期・バリデーション |
| **合計** | **21 件** | |

### Go ユニットテスト（AWS 接続不要）

Go Lambda（`lambda_go/`）のヘルパー関数・バリデーションロジックを検証します。

```bash
cd lambda_go
go test ./... -v
```

| テストファイル | テスト数 | 主な検証内容 |
|---|---|---|
| `lambda_go/main_test.go` | 10 件 | 環境変数取得・API レスポンス生成・Handler 入力バリデーション |

---

### Lambda の手動呼び出し

```bash
aws-vault exec personal-dev-source -- aws lambda invoke \
  --function-name $(cd terraform && terraform output -raw lambda_function_name) \
  --payload '{"query": "有給休暇の申請方法を教えてください"}' \
  response.json
cat response.json
```

### ドキュメント更新後の動作確認

```bash
aws-vault exec personal-dev-source -- aws s3 cp knowledge.txt \
  s3://$(cd terraform && terraform output -raw s3_bucket_name)/documents/knowledge.txt
```

---

## CI / セキュリティスキャン

GitHub Actions で Python リント（flake8）と Terraform の静的解析（Checkov）を自動実行しています。

### 実施内容

| ジョブ | 内容 |
|---|---|
| Python lint（flake8） | コードスタイル・構文エラーの検出 |
| terraform fmt / validate | フォーマット・構文チェック |
| Checkov セキュリティスキャン | IaC のセキュリティポリシー違反を検出（soft_fail: false） |

### セキュリティ対応（Terraform で修正した内容）

| リソース | 追加設定 |
|---|---|
| S3（documents バケット） | SSE-AES256 暗号化・パブリックアクセスブロック（4項目）・バージョニング・ライフサイクル（90日削除 + multipart abort 7日） |
| Lambda | `tracing_config { mode = "PassThrough" }`（X-Ray 有効化） |
| IAM（Bedrock ポリシー） | `Resource = "*"` → 特定モデル ARN に限定 |
| CloudWatch Logs | 保持期間のデフォルトを 30 日に設定 |

### 意図的にスキップしている項目（PoC の合理的な省略）

| チェック ID | 内容 | 理由 |
|---|---|---|
| CKV_AWS_117 | Lambda VPC 内配置 | dev/PoC では不要 |
| CKV_AWS_272 | Lambda コード署名 | dev/PoC では不要 |
| CKV_AWS_116 | Lambda DLQ 設定 | dev/PoC では不要 |
| CKV_AWS_115 | Lambda 予約済み同時実行 | dev/PoC では不要 |
| CKV_AWS_173 | Lambda 環境変数 KMS | dev/PoC では不要 |
| CKV_AWS_158 | CloudWatch Logs KMS | dev/PoC では不要 |
| CKV_AWS_338 | CloudWatch Logs 保持期間 1 年未満 | dev は 30 日で十分 |
| CKV_AWS_145 | S3 KMS 暗号化 | AES256 で十分 |
| CKV_AWS_18 | S3 アクセスログ | dev/PoC では不要 |
| CKV_AWS_144 | S3 クロスリージョンレプリケーション | dev/PoC では不要 |
| CKV2_AWS_62 | S3 通知設定 | dev/PoC では不要 |
| API Gateway 系（複数） | 認証・WAF・アクセスログ・X-Ray・キャッシュ | dev/PoC では不要 |
| CKV_AWS_111 / CKV_AWS_356（インライン） | aws-marketplace Resource `"*"` | AWS がリソースレベル制限を非対応 |

---

## AI 活用について

本プロジェクトは以下の Anthropic ツールを活用して開発しています。

| ツール | 用途 |
|---|---|
| **Claude Code** | インフラ設計・コード生成・デバッグ・コードレビュー。コミットまで一貫してサポート |
| **Claude Cowork** | 技術調査・設計相談・ドキュメント作成を日常的に活用。AI との協働を業務フローに組み込んでいる |
| **カスタム Skills** | Terraform / Python / AWS に特化した Skills を設定・継続的に更新。自分の技術スタックに最適化したワークフローを構築 |

> AI を「使う」だけでなく、自分の業務・技術スタックに合わせて**設定・運用・改善し続ける**ことを意識しています。

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting and security policies.
