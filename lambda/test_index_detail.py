"""
index.py 追加ユニットテスト（詳細ケース）

S3 キャッシュ・Bedrock エラー網羅・ハンドラーヘッダー検証・
8000 文字制限・dict 形式 body など、test_index.py の補完テスト。
"""

import json
from unittest.mock import MagicMock, patch

import index
import pytest
from botocore.exceptions import ClientError
from index import get_document_from_s3, handler, invoke_bedrock


# ── get_document_from_s3 追加ケース ──────────────────────────────────
class TestGetDocumentFromS3Detail:
    def setup_method(self):
        index._cached_document = None

    @patch("index._s3_client")
    def test_キャッシュリセット後は再度S3を呼ぶ(self, mock_s3):
        """キャッシュをリセットした後は S3 を再呼び出しすること"""
        mock_s3.get_object.return_value = {
            "Body": MagicMock(read=lambda: b"fresh content")
        }
        # 1回目
        first = get_document_from_s3("bucket", "key.txt")
        # キャッシュをリセット
        index._cached_document = None
        # 2回目
        second = get_document_from_s3("bucket", "key.txt")
        assert first == second == "fresh content"
        assert mock_s3.get_object.call_count == 2

    @patch("index._s3_client")
    def test_NoSuchKey以外のClientErrorは例外を再送出する(self, mock_s3):
        """AccessDenied 等の予期しないエラーは例外として伝播すること"""
        error = ClientError(
            {"Error": {"Code": "AccessDeniedException", "Message": ""}}, "GetObject"
        )
        mock_s3.get_object.side_effect = error
        with pytest.raises(ClientError):
            get_document_from_s3("bucket", "key.txt")

    @patch("index._s3_client")
    def test_取得したドキュメントがキャッシュに保存される(self, mock_s3):
        """取得したドキュメントが _cached_document にセットされること"""
        mock_s3.get_object.return_value = {
            "Body": MagicMock(read=lambda: b"stored in cache")
        }
        get_document_from_s3("bucket", "key.txt")
        assert index._cached_document == "stored in cache"


# ── invoke_bedrock 追加ケース ─────────────────────────────────────────
class TestInvokeBedrockDetail:
    @patch("index._bedrock_client")
    def test_その他エラーは汎用メッセージを返す(self, mock_bedrock):
        """不明なエラーコードでも汎用メッセージが返ること"""
        error = ClientError(
            {"Error": {"Code": "InternalServerError", "Message": ""}}, "InvokeModel"
        )
        mock_bedrock.invoke_model.side_effect = error
        result = invoke_bedrock("", "質問")
        assert "失敗" in result or "確認" in result

    @patch("index._bedrock_client")
    def test_ドキュメント8000文字制限が適用される(self, mock_bedrock):
        """8001 文字のドキュメントが 8000 文字に切り詰められてプロンプトに渡ること"""
        mock_bedrock.invoke_model.return_value = {
            "body": MagicMock(
                read=lambda: json.dumps({"content": [{"text": "ok"}]}).encode()
            )
        }
        long_doc = "a" * 8001
        invoke_bedrock(long_doc, "質問")

        call_args = mock_bedrock.invoke_model.call_args
        body = json.loads(call_args[1]["body"])
        user_content = body["messages"][0]["content"]
        # 8001文字のdocは8000文字に切り詰め → プロンプト内に "a"*8001 は存在しない
        assert "a" * 8001 not in user_content
        assert "a" * 8000 in user_content

    @patch("index._bedrock_client")
    def test_ドキュメントなし時のプロンプトに社内ドキュメントなし文言が含まれる(
        self, mock_bedrock
    ):
        """ドキュメントが空の場合、プロンプトに「見つかりませんでした」が含まれること"""
        mock_bedrock.invoke_model.return_value = {
            "body": MagicMock(
                read=lambda: json.dumps({"content": [{"text": "回答"}]}).encode()
            )
        }
        invoke_bedrock("", "質問")

        call_args = mock_bedrock.invoke_model.call_args
        body = json.loads(call_args[1]["body"])
        user_content = body["messages"][0]["content"]
        assert "見つかりませんでした" in user_content


# ── handler 追加ケース ────────────────────────────────────────────────
class TestHandlerDetail:
    def test_質問が改行のみは400を返す(self):
        event = {"body": json.dumps({"question": "\n\n\n"})}
        result = handler(event, None)
        assert result["statusCode"] == 400

    def test_質問がタブのみは400を返す(self):
        event = {"body": json.dumps({"question": "\t\t"})}
        result = handler(event, None)
        assert result["statusCode"] == 400

    def test_bodyがdict形式でも動作する(self):
        """body が文字列ではなく dict で渡された場合にも 400 が正しく返ること"""
        event = {"body": {"question": ""}}
        result = handler(event, None)
        assert result["statusCode"] == 400

    def test_400レスポンスにCORSヘッダーが含まれる(self):
        event = {"body": "not-json"}
        result = handler(event, None)
        assert result["statusCode"] == 400
        assert result["headers"]["Access-Control-Allow-Origin"] == "*"

    def test_400レスポンスにContentTypeヘッダーが含まれる(self):
        event = {"body": json.dumps({"question": ""})}
        result = handler(event, None)
        assert result["statusCode"] == 400
        assert result["headers"]["Content-Type"] == "application/json"

    @patch("index.invoke_bedrock", return_value="回答")
    @patch("index.get_document_from_s3", return_value="doc")
    @patch("index.S3_BUCKET_NAME", "test-bucket")
    def test_200レスポンスにContentTypeヘッダーが含まれる(self, mock_s3, mock_bedrock):
        event = {"body": json.dumps({"question": "テスト"})}
        result = handler(event, None)
        assert result["statusCode"] == 200
        assert result["headers"]["Content-Type"] == "application/json"

    @patch("index.invoke_bedrock", return_value="回答")
    @patch("index.get_document_from_s3", return_value="doc")
    @patch("index.S3_BUCKET_NAME", "test-bucket")
    def test_200レスポンスのbodyが有効なJSON(self, mock_s3, mock_bedrock):
        event = {"body": json.dumps({"question": "テスト"})}
        result = handler(event, None)
        assert result["statusCode"] == 200
        body = json.loads(result["body"])
        assert "answer" in body
        assert "source" in body
