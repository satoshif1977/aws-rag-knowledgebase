"""
aws-rag-knowledgebase Lambda ユニットテスト

S3・Bedrock 呼び出しをモックし、AWS 接続なしでロジックを検証する。
"""

import json
from unittest.mock import MagicMock, patch

from botocore.exceptions import ClientError

from index import get_document_from_s3, handler, invoke_bedrock


# ── get_document_from_s3 テスト ───────────────────────────
class TestGetDocumentFromS3:
    @patch("index.boto3.client")
    def test_正常系_ドキュメントを取得できる(self, mock_client):
        mock_s3 = MagicMock()
        mock_client.return_value = mock_s3
        mock_s3.get_object.return_value = {
            "Body": MagicMock(read=lambda: b"sample document text")
        }
        result = get_document_from_s3("my-bucket", "docs/test.txt")
        assert result == "sample document text"

    @patch("index.boto3.client")
    def test_NoSuchKey_の場合は空文字を返す(self, mock_client):
        mock_s3 = MagicMock()
        mock_client.return_value = mock_s3
        error = ClientError({"Error": {"Code": "NoSuchKey", "Message": ""}}, "GetObject")
        mock_s3.get_object.side_effect = error
        result = get_document_from_s3("my-bucket", "not-exist.txt")
        assert result == ""


# ── invoke_bedrock テスト ─────────────────────────────────
class TestInvokeBedrock:
    @patch("index.boto3.client")
    def test_正常系_回答を返す(self, mock_client):
        mock_bedrock = MagicMock()
        mock_client.return_value = mock_bedrock
        mock_bedrock.invoke_model.return_value = {
            "body": MagicMock(
                read=lambda: json.dumps(
                    {"content": [{"text": "有給休暇は社内ポータルから申請できます。"}]}
                ).encode()
            )
        }
        result = invoke_bedrock("社内規定テキスト", "有給休暇の申請方法は？")
        assert "有給休暇" in result

    @patch("index.boto3.client")
    def test_AccessDenied_の場合はメッセージを返す(self, mock_client):
        mock_bedrock = MagicMock()
        mock_client.return_value = mock_bedrock
        error = ClientError(
            {"Error": {"Code": "AccessDeniedException", "Message": ""}}, "InvokeModel"
        )
        mock_bedrock.invoke_model.side_effect = error
        result = invoke_bedrock("", "質問")
        assert "利用できません" in result

    @patch("index.boto3.client")
    def test_Throttling_の場合はメッセージを返す(self, mock_client):
        mock_bedrock = MagicMock()
        mock_client.return_value = mock_bedrock
        error = ClientError(
            {"Error": {"Code": "ThrottlingException", "Message": ""}}, "InvokeModel"
        )
        mock_bedrock.invoke_model.side_effect = error
        result = invoke_bedrock("", "質問")
        assert "集中" in result

    @patch("index.boto3.client")
    def test_ドキュメントなしでも回答する(self, mock_client):
        mock_bedrock = MagicMock()
        mock_client.return_value = mock_bedrock
        mock_bedrock.invoke_model.return_value = {
            "body": MagicMock(
                read=lambda: json.dumps(
                    {"content": [{"text": "一般的な回答です。"}]}
                ).encode()
            )
        }
        result = invoke_bedrock("", "質問")
        assert result == "一般的な回答です。"


# ── handler テスト ────────────────────────────────────────
class TestHandler:
    @patch("index.invoke_bedrock", return_value="テスト回答")
    @patch("index.get_document_from_s3", return_value="社内規定テキスト")
    @patch("index.S3_BUCKET_NAME", "test-bucket")
    def test_正常系_200とanswerを返す(self, mock_s3, mock_bedrock):
        event = {"body": json.dumps({"question": "有給休暇の申請方法は？"})}
        result = handler(event, None)
        assert result["statusCode"] == 200
        body = json.loads(result["body"])
        assert body["answer"] == "テスト回答"
        assert body["source"] == "s3_document"

    def test_質問が空の場合は400を返す(self):
        event = {"body": json.dumps({"question": ""})}
        result = handler(event, None)
        assert result["statusCode"] == 400

    def test_不正なJSONは400を返す(self):
        event = {"body": "not a json"}
        result = handler(event, None)
        assert result["statusCode"] == 400
