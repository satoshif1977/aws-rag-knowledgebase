"""
ingestion_handler ユニットテスト（AWS 接続なし）
"""

import json
import os
import sys
from unittest.mock import MagicMock, patch

os.environ.setdefault("AWS_REGION", "ap-northeast-1")
sys.path.insert(0, os.path.dirname(__file__))

from ingestion_handler import _get_extension, handler  # noqa: E402


# ── ヘルパー ──────────────────────────────────────────────────────────
def _make_sqs_record(bucket: str, key: str, size: int = 1024) -> dict:
    """SQS 経由の S3 ObjectCreated イベント通知レコードを生成するヘルパー"""
    return {
        "messageId": "msg-test-001",
        "receiptHandle": "receipt-test",
        "body": json.dumps(
            {
                "Records": [
                    {
                        "eventVersion": "2.1",
                        "eventName": "ObjectCreated:Put",
                        "s3": {
                            "bucket": {"name": bucket},
                            "object": {"key": key, "size": size},
                        },
                    }
                ]
            }
        ),
    }


def _mock_s3_head(content_length: int = 1024, content_type: str = "application/pdf"):
    """S3 get_object のモックレスポンスを生成するヘルパー"""
    return {
        "ContentLength": content_length,
        "ContentType": content_type,
        "Body": MagicMock(),
    }


# ── _get_extension のテスト ───────────────────────────────────────────
class TestGetExtension:
    def test_pdf拡張子を返す(self):
        assert _get_extension("docs/report.pdf") == ".pdf"

    def test_大文字でも小文字で返す(self):
        assert _get_extension("DOCUMENT.PDF") == ".pdf"

    def test_拡張子なしは空文字(self):
        assert _get_extension("README") == ""

    def test_ネストしたパスでも拡張子を取得(self):
        assert _get_extension("a/b/c/file.md") == ".md"

    def test_docx拡張子を返す(self):
        assert _get_extension("manual.docx") == ".docx"


# ── handler のテスト ─────────────────────────────────────────────────
class TestHandler:
    @patch("ingestion_handler._s3_client")
    def test_pdf取り込みが成功する(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_head(2048, "application/pdf")

        result = handler(
            [_make_sqs_record("my-bucket", "docs/report.pdf")], MagicMock()
        )

        assert result["total"] == 1
        assert result["errors"] == []
        assert result["processed"][0]["status"] == "success"
        assert result["processed"][0]["key"] == "docs/report.pdf"
        assert result["processed"][0]["bucket"] == "my-bucket"

    @patch("ingestion_handler._s3_client")
    def test_txt取り込みが成功する(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_head(512, "text/plain")

        result = handler(
            [_make_sqs_record("my-bucket", "docs/readme.txt")], MagicMock()
        )

        assert result["total"] == 1
        assert result["processed"][0]["status"] == "success"

    @patch("ingestion_handler._s3_client")
    def test_urlエンコードされたキーがデコードされる(self, mock_s3):
        """S3 イベント通知のキーは URL エンコードされているためデコードされること"""
        mock_s3.get_object.return_value = _mock_s3_head(1000, "text/plain")
        # "社内規程.txt" を URL エンコード
        encoded_key = "%E7%A4%BE%E5%86%85%E8%A6%8F%E7%A8%8B.txt"

        result = handler([_make_sqs_record("my-bucket", encoded_key)], MagicMock())

        assert result["total"] == 1
        assert result["processed"][0]["key"] == "社内規程.txt"

    @patch("ingestion_handler._s3_client")
    def test_サポート外の拡張子はスキップされる(self, mock_s3):
        """Pipes フィルターをすり抜けた非対応ファイルはスキップされること"""
        result = handler([_make_sqs_record("my-bucket", "image.jpg")], MagicMock())

        mock_s3.get_object.assert_not_called()
        assert result["total"] == 0
        assert result["skipped"][0]["status"] == "skipped"

    @patch("ingestion_handler._s3_client")
    def test_s3エラーはerrors配列に格納される(self, mock_s3):
        from botocore.exceptions import ClientError

        mock_s3.get_object.side_effect = ClientError(
            {"Error": {"Code": "NoSuchKey", "Message": "Not found"}},
            "GetObject",
        )

        result = handler([_make_sqs_record("my-bucket", "missing.pdf")], MagicMock())

        assert result["total"] == 0
        assert result["errors"][0]["status"] == "error"
        assert result["errors"][0]["reason"] == "NoSuchKey"

    def test_不正なjsonボディはerrors配列に格納される(self):
        event = [{"messageId": "bad", "body": "not-valid-json"}]

        result = handler(event, MagicMock())

        assert result["total"] == 0
        assert len(result["errors"]) == 1

    @patch("ingestion_handler._s3_client")
    def test_dict形式のイベントでも動作する(self, mock_s3):
        """Pipes ではなく直接 Lambda を invoke した場合（dict 形式）でも動作すること"""
        mock_s3.get_object.return_value = _mock_s3_head(100, "text/plain")
        # dict（リストでない）
        event = _make_sqs_record("my-bucket", "notes.md")

        result = handler(event, MagicMock())

        assert result["total"] == 1
        assert result["processed"][0]["status"] == "success"

    @patch("ingestion_handler._s3_client")
    def test_md取り込みが成功する(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_head(300, "text/markdown")

        result = handler([_make_sqs_record("my-bucket", "README.md")], MagicMock())

        assert result["total"] == 1
        assert result["processed"][0]["status"] == "success"

    @patch("ingestion_handler._s3_client")
    def test_複数レコードを一括処理できる(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_head(1024, "application/pdf")

        result = handler(
            [
                _make_sqs_record("bucket", "a.pdf"),
                _make_sqs_record("bucket", "b.txt"),
            ],
            MagicMock(),
        )

        assert result["total"] == 2
        assert len(result["errors"]) == 0

    def test_拡張子なしファイルはスキップされる(self):
        result = handler([_make_sqs_record("my-bucket", "Makefile")], MagicMock())

        assert result["total"] == 0
        assert result["skipped"][0]["status"] == "skipped"

    @patch("ingestion_handler._s3_client")
    def test_空のRecords配列は空の結果を返す(self, mock_s3):
        event = [{"messageId": "m1", "body": json.dumps({"Records": []})}]

        result = handler(event, MagicMock())

        assert result["total"] == 0
        assert result["errors"] == []
        assert result["skipped"] == []

    @patch("ingestion_handler._s3_client")
    def test_docx取り込みが成功する(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_head(
            4096,
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        )

        result = handler([_make_sqs_record("my-bucket", "manual.docx")], MagicMock())

        assert result["total"] == 1
        assert result["processed"][0]["status"] == "success"
