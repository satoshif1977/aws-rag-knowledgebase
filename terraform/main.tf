# ── aws-rag-knowledgebase ──────────────────────────────────
# 構成:
#   S3（ドキュメント格納）
#   Lambda（PDF テキスト取得 + Bedrock 呼び出し）
#   API Gateway（REST API / POST /query）← aws-bedrock-agent との差別化
#   IAM Role（Lambda 実行権限）
#   CloudWatch Logs（ログ保存）
# ──────────────────────────────────────────────────────────

# ── Lambda デプロイパッケージ ──────────────────────────────
data "archive_file" "lambda" {
  type        = "zip"
  source_dir  = "${path.module}/../lambda"
  output_path = "${path.module}/../lambda.zip"
}

# ── S3 バケット（ドキュメント格納） ───────────────────────
resource "aws_s3_bucket" "documents" {
  bucket = "${var.project_name}-${var.environment}-documents-${data.aws_caller_identity.current.account_id}"
}

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket_versioning" "documents" {
  bucket = aws_s3_bucket.documents.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "documents" {
  bucket                  = aws_s3_bucket.documents.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ── IAM ロール ─────────────────────────────────────────────
resource "aws_iam_role" "lambda" {
  name        = "${var.project_name}-${var.environment}-lambda-role"
  description = "IAM role for RAG Knowledgebase Lambda"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "lambda_permissions" {
  name = "${var.project_name}-${var.environment}-lambda-policy"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # Bedrock 呼び出し権限
        Effect   = "Allow"
        Action   = ["bedrock:InvokeModel"]
        Resource = "*"
        # TODO: 本番では使用するモデル ARN に絞る
      },
      {
        # S3 読み取り権限（ドキュメントバケットのみ）
        Effect = "Allow"
        Action = ["s3:GetObject", "s3:ListBucket"]
        Resource = [
          aws_s3_bucket.documents.arn,
          "${aws_s3_bucket.documents.arn}/*"
        ]
      },
      {
        # SSM Parameter Store 読み取り権限
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:GetParameters"]
        Resource = "arn:aws:ssm:${var.aws_region}:*:parameter/${var.project_name}/*"
      }
    ]
  })
}

# ── CloudWatch Logs ────────────────────────────────────────
resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${var.project_name}-${var.environment}"
  retention_in_days = var.log_retention_days
}

# ── Lambda 関数 ────────────────────────────────────────────
resource "aws_lambda_function" "main" {
  function_name = "${var.project_name}-${var.environment}"
  description   = "社内規定 PDF Q&A - RAG PoC（S3 + API Gateway + Bedrock）"
  role          = aws_iam_role.lambda.arn
  handler       = "index.handler"
  runtime       = "python3.11"
  timeout       = var.lambda_timeout
  memory_size   = var.lambda_memory_size

  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256

  environment {
    variables = {
      BEDROCK_MODEL_ID = var.bedrock_model_id
      S3_BUCKET_NAME   = aws_s3_bucket.documents.bucket
      S3_PDF_KEY       = var.s3_pdf_key
      LOG_LEVEL        = "INFO"
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.lambda_basic,
    aws_cloudwatch_log_group.lambda,
  ]
}

# ── API Gateway（REST API） ────────────────────────────────
resource "aws_api_gateway_rest_api" "main" {
  name        = "${var.project_name}-${var.environment}-api"
  description = "社内規定 PDF Q&A API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_resource" "query" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "query"
}

resource "aws_api_gateway_method" "post_query" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.query.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.query.id
  http_method             = aws_api_gateway_method.post_query.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.main.invoke_arn
}

resource "aws_api_gateway_deployment" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id

  depends_on = [aws_api_gateway_integration.lambda]

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.main.id
  rest_api_id   = aws_api_gateway_rest_api.main.id
  stage_name    = var.environment
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.main.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*/*"
}
