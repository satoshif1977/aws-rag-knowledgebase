# ── aws-rag-knowledgebase / EventBridge Pipes ────────────────────────────────
# SQS（取り込みキュー + DLQ）→ EventBridge Pipes（拡張子フィルター）→ Lambda
#
# フロー:
#   S3（ドキュメントアップロード）
#     → S3 イベント通知（ObjectCreated）
#       → SQS（document-ingestion キュー）
#         → EventBridge Pipes（.pdf / .txt / .md / .docx のみ通過）
#           → Lambda（ingestion_handler）: ドキュメント取り込み処理
#
# ポイント: Pipes のフィルターにより非対応ファイルは Lambda を起動せず破棄される
#           → 無駄な Lambda 起動を防ぎコストを最適化
# ─────────────────────────────────────────────────────────────────────────────

# ── SQS Dead Letter Queue ─────────────────────────────────────────────────────
resource "aws_sqs_queue" "ingestion_dlq" {
  name                    = "${var.project_name}-${var.environment}-ingestion-dlq"
  sqs_managed_sse_enabled = true
}

# ── SQS ドキュメント取り込みキュー ───────────────────────────────────────────
resource "aws_sqs_queue" "ingestion" {
  name                       = "${var.project_name}-${var.environment}-ingestion"
  sqs_managed_sse_enabled    = true
  visibility_timeout_seconds = 180 # Lambda タイムアウト (30s) × 6 倍が推奨

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.ingestion_dlq.arn
    maxReceiveCount     = 3
  })
}

# ── SQS キューポリシー（S3 からのイベント通知送信を許可） ─────────────────────
resource "aws_sqs_queue_policy" "ingestion" {
  queue_url = aws_sqs_queue.ingestion.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "AllowS3EventNotification"
      Effect    = "Allow"
      Principal = { Service = "s3.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.ingestion.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_s3_bucket.documents.arn }
      }
    }]
  })
}

# ── S3 イベント通知（ObjectCreated → SQS） ────────────────────────────────────
resource "aws_s3_bucket_notification" "documents" {
  bucket = aws_s3_bucket.documents.id

  queue {
    id        = "document-created"
    queue_arn = aws_sqs_queue.ingestion.arn
    events    = ["s3:ObjectCreated:*"]
  }

  depends_on = [aws_sqs_queue_policy.ingestion]
}

# ── ドキュメント取り込み Lambda（CloudWatch Logs グループ） ───────────────────
resource "aws_cloudwatch_log_group" "ingestion_lambda" {
  name              = "/aws/lambda/${var.project_name}-${var.environment}-ingestion"
  retention_in_days = var.log_retention_days
}

# ── ドキュメント取り込み Lambda（IAM ロール） ─────────────────────────────────
resource "aws_iam_role" "ingestion_lambda" {
  name        = "${var.project_name}-${var.environment}-ingestion-lambda-role"
  description = "IAM role for RAG document ingestion Lambda (EventBridge Pipes)"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ingestion_lambda_basic" {
  role       = aws_iam_role.ingestion_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "ingestion_lambda_permissions" {
  name = "${var.project_name}-${var.environment}-ingestion-lambda-policy"
  role = aws_iam_role.ingestion_lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "ReadDocuments"
      Effect   = "Allow"
      Action   = ["s3:GetObject"]
      Resource = "${aws_s3_bucket.documents.arn}/*"
    }]
  })
}

# ── ドキュメント取り込み Lambda（デプロイパッケージ） ────────────────────────
data "archive_file" "ingestion_lambda" {
  type        = "zip"
  source_file = "${path.module}/../lambda/ingestion_handler.py"
  output_path = "${path.module}/../lambda_ingestion.zip"
}

# ── ドキュメント取り込み Lambda 関数 ──────────────────────────────────────────
resource "aws_lambda_function" "ingestion" {
  function_name = "${var.project_name}-${var.environment}-ingestion"
  description   = "S3 ドキュメント取り込みハンドラー（EventBridge Pipes 経由で起動）"
  role          = aws_iam_role.ingestion_lambda.arn
  handler       = "ingestion_handler.handler"
  runtime       = "python3.11"
  timeout       = 30
  memory_size   = 128

  filename         = data.archive_file.ingestion_lambda.output_path
  source_code_hash = data.archive_file.ingestion_lambda.output_base64sha256

  environment {
    variables = {
      S3_BUCKET_NAME = aws_s3_bucket.documents.bucket
      LOG_LEVEL      = "INFO"
    }
  }

  tracing_config {
    mode = "PassThrough"
  }

  depends_on = [
    aws_iam_role_policy_attachment.ingestion_lambda_basic,
    aws_cloudwatch_log_group.ingestion_lambda,
  ]
}

# ── EventBridge Pipes IAM ─────────────────────────────────────────────────────
resource "aws_iam_role" "pipes" {
  name        = "${var.project_name}-${var.environment}-pipes-role"
  description = "IAM role for EventBridge Pipes (SQS source → Lambda target)"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "pipes.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "pipes" {
  name = "${var.project_name}-${var.environment}-pipes-policy"
  role = aws_iam_role.pipes.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ReadFromSQS"
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes",
        ]
        Resource = aws_sqs_queue.ingestion.arn
      },
      {
        Sid      = "InvokeIngestionLambda"
        Effect   = "Allow"
        Action   = ["lambda:InvokeFunction"]
        Resource = aws_lambda_function.ingestion.arn
      },
    ]
  })
}

# ── EventBridge Pipe（SQS → 拡張子フィルター → Lambda）──────────────────────
resource "aws_pipes_pipe" "document_ingestion" {
  name     = "${var.project_name}-${var.environment}-document-ingestion"
  role_arn = aws_iam_role.pipes.arn

  # ソース: SQS ドキュメント取り込みキュー
  source = aws_sqs_queue.ingestion.arn

  source_parameters {
    sqs_queue_parameters {
      batch_size                         = 1
      maximum_batching_window_in_seconds = 0
    }

    filter_criteria {
      filter {
        # .pdf / .txt / .md / .docx のみ通過（他の拡張子はここで破棄・Lambda 起動なし）
        # S3 イベント通知の body.Records[].s3.object.key を suffix でマッチング
        pattern = jsonencode({
          body = {
            Records = [{
              s3 = {
                object = {
                  key = [
                    { suffix = ".pdf" },
                    { suffix = ".txt" },
                    { suffix = ".md" },
                    { suffix = ".docx" },
                  ]
                }
              }
            }]
          }
        })
      }
    }
  }

  # ターゲット: ドキュメント取り込み Lambda
  target = aws_lambda_function.ingestion.arn
}
