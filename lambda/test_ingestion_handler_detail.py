"""
ingestion_handler.py 追加ユニットテスト（詳細ケース）

拡張子バリエーション・処理結果の詳細フィールド検証・
混在レコード・エッジケースなど、test_ingestion_handler.py の補完テスト。
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
    return {
        "messageId": "msg-detail-001",
        "receiptHandle": "receipt-detail",
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


def _mock_s3_response(
    content_length: int = 1024, content_type: str = "application/pdf"
) -> dict:
    return {
        "ContentLength": content_length,
        "ContentType": content_type,
        "Body": MagicMock(),
    }


# ── _get_extension 追加ケース ─────────────────────────────────────────
class TestGetExtensionDetail:
    def test_txt拡張子を返す(self):
        assert _get_extension("readme.txt") == ".txt"

    def test_md拡張子を返す(self):
        assert _get_extension("notes.md") == ".md"

    def test_ドットから始まる隠しファイルは空文字(self):
        # os.path.splitext(".gitignore") → ('.gitignore', '')
        assert _get_extension(".gitignore") == ""

    def test_複数ドットのファイル名は最後の拡張子(self):
        # "archive.tar.gz" → ext = ".gz"（SUPPORTED_EXTENSIONS にないのでスキップ対象）
        assert _get_extension("archive.tar.gz") == ".gz"

    def test_スペースを含むキーも処理できる(self):
        assert _get_extension("my document.pdf") == ".pdf"


# ── handler 処理結果の詳細フィールド検証 ─────────────────────────────
class TestHandlerResultDetail:
    @patch("ingestion_handler._s3_client")
    def test_処理済みリストのsize情報が正しい(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_response(2048, "text/plain")
        result = handler([_make_sqs_record("bucket", "doc.txt", 2048)], MagicMock())
        assert result["processed"][0]["size"] == 2048

    @patch("ingestion_handler._s3_client")
    def test_処理済みリストのcontent_typeが正しい(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_response(
            1024, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        )
        result = handler([_make_sqs_record("bucket", "manual.docx")], MagicMock())
        assert "wordprocessingml" in result["processed"][0]["content_type"]

    @patch("ingestion_handler._s3_client")
    def test_skippedリストにreasonフィールドが含まれる(self, mock_s3):
        result = handler([_make_sqs_record("bucket", "image.png")], MagicMock())
        assert "reason" in result["skipped"][0]
        assert ".png" in result["skipped"][0]["reason"]

    @patch("ingestion_handler._s3_client")
    def test_処理済みリストにbucketフィールドが含まれる(self, mock_s3):
        mock_s3.get_object.return_value = _mock_s3_response(512, "text/plain")
        result = handler([_make_sqs_record("my-bucket", "file.txt")], MagicMock())
        assert result["processed"][0]["bucket"] == "my-bucket"


# ── handler エッジケース ──────────────────────────────────────────────
class TestHandlerEdgeCases:
    def test_空のeventリストは空の結果(self):
        result = handler([], MagicMock())
        assert result["total"] == 0
        assert result["processed"] == []
        assert result["skipped"] == []
        assert result["errors"] == []

    def test_pngはスキップされる(self):
        result = handler([_make_sqs_record("bucket", "photo.png")], MagicMock())
        assert result["total"] == 0
        assert result["skipped"][0]["status"] == "skipped"

    def test_Records_キーなしSQSメッセージはエラーなし(self):
        """Records キーがない SQS メッセージは空の Records として扱われること"""
        event = [{"messageId": "m1", "body": json.dumps({"other": "data"})}]
        result = handler(event, MagicMock())
        assert result["total"] == 0
        assert result["errors"] == []

    @patch("ingestion_handler._s3_client")
    def test_成功_スキップ_エラー混在の合計が正しい(self, mock_s3):
        """成功・スキップ・エラーが混在する場合に各カウントが正しいこと"""
        from botocore.exceptions import ClientError

        call_count = [0]

        def side_effect(**kwargs):
            call_count[0] += 1
            if call_count[0] == 1:
                return _mock_s3_response(100, "text/plain")
            raise ClientError(
                {"Error": {"Code": "NoSuchKey", "Message": ""}}, "GetObject"
            )

        mock_s3.get_object.side_effect = side_effect

        result = handler(
            [
                _make_sqs_record("bucket", "a.txt"),   # success
                _make_sqs_record("bucket", "b.jpg"),   # skipped（非対応拡張子）
                _make_sqs_record("bucket", "c.pdf"),   # error（S3 エラー）
            ],
            MagicMock(),
        )

        assert len(result["processed"]) == 1
        assert len(result["skipped"]) == 1
        assert len(result["errors"]) == 1
