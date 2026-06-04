output "api_gateway_url" {
  description = "API Gateway エンドポイント URL"
  value       = "${aws_api_gateway_stage.dev.invoke_url}/query"
}

output "s3_bucket_name" {
  description = "ドキュメント格納 S3 バケット名"
  value       = aws_s3_bucket.documents.bucket
}

output "lambda_function_name" {
  description = "Lambda 関数名"
  value       = aws_lambda_function.main.function_name
}

output "cloudwatch_logs_url" {
  description = "CloudWatch Logs URL"
  value       = "https://ap-northeast-1.console.aws.amazon.com/cloudwatch/home?region=ap-northeast-1#logsV2:log-groups/log-group/$252Faws$252Flambda$252F${aws_lambda_function.main.function_name}"
}

output "ingestion_queue_url" {
  description = "ドキュメント取り込み SQS キュー URL"
  value       = aws_sqs_queue.ingestion.url
}

output "ingestion_dlq_url" {
  description = "ドキュメント取り込み DLQ URL（失敗メッセージの確認用）"
  value       = aws_sqs_queue.ingestion_dlq.url
}

output "ingestion_pipe_name" {
  description = "EventBridge Pipe 名（拡張子フィルター付きドキュメント取り込み）"
  value       = aws_pipes_pipe.document_ingestion.name
}

output "ingestion_lambda_name" {
  description = "ドキュメント取り込み Lambda 関数名"
  value       = aws_lambda_function.ingestion.function_name
}
