variable "project_name" {
  description = "プロジェクト名（リソース命名に使用）"
  type        = string
  default     = "rag-kb"
}

variable "environment" {
  description = "環境名（dev / stg / prod）"
  type        = string
  default     = "dev"
}

variable "aws_region" {
  description = "デプロイ先 AWS リージョン"
  type        = string
  default     = "ap-northeast-1"
}

variable "bedrock_model_id" {
  description = "使用する Bedrock モデル ID"
  type        = string
  default     = "anthropic.claude-3-haiku-20240307-v1:0"
}

variable "lambda_timeout" {
  description = "Lambda タイムアウト秒数（Bedrock の応答時間を考慮）"
  type        = number
  default     = 60
}

variable "lambda_memory_size" {
  description = "Lambda メモリサイズ（MB）"
  type        = number
  default     = 256
}

variable "log_retention_days" {
  description = "CloudWatch Logs の保持日数（dev: 30, prod: 90 を推奨）"
  type        = number
  default     = 30
}

variable "s3_pdf_key" {
  description = "S3 に格納するドキュメントのキー名"
  type        = string
  default     = "documents/knowledge.txt"
}
