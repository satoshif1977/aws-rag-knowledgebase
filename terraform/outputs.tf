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
