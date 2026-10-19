"""
logger.py / metrics.py / retry.py が実際に結線されていることを検証する。

ユーティリティを置いただけでは意味がない。このファイルでは次を確かめる。

  1. Bedrock のスロットリングが自動リトライされ、利用者にエラーを返さないこと
     （従来は 1 回失敗しただけで「しばらくしてから再度お試しください」を返していた）
  2. 質問文・回答文がログに残らないこと（社内規定 Q&A のため）
  3. リトライがログとメトリクスの両方に記録されること
  4. EMF メトリクスが実際に出力されること
"""

from __future__ import annotations

import json
import os
import sys
from typing import Any
from unittest.mock import MagicMock, patch

import pytest
from botocore.exceptions import ClientError

sys.path.insert(0, os.path.dirname(__file__))

import index  # noqa: E402
import ingestion_handler  # noqa: E402
import wiring  # noqa: E402
from logger import create_logger  # noqa: E402
from metrics import create_metrics  # noqa: E402

# ── ヘルパー ───────────────────────────────────────────────


class _Ctx:
    aws_request_id = "req-0001"


def api_event(question: str = "有給休暇は何日ですか") -> dict[str, Any]:
    return {"body": json.dumps({"question": question}, ensure_ascii=False)}


def throttling(op: str = "InvokeModel") -> ClientError:
    return ClientError(
        {"Error": {"Code": "ThrottlingException", "Message": "slow down"}}, op
    )


def bedrock_response(text: str = "20日です") -> dict[str, Any]:
    body = MagicMock()
    body.read.return_value = json.dumps({"content": [{"text": text}]}).encode()
    return {"body": body}


def json_lines(captured: Any) -> list[dict[str, Any]]:
    """1 行 JSON だけを拾う。warn / error は標準エラーへ出るため両方読む。"""
    if hasattr(captured, "out"):
        text = captured.out + chr(10) + captured.err
    else:
        text = str(captured)
    out = []
    for line in text.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            out.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    return out


@pytest.fixture(autouse=True)
def _reset_cache():
    """S3 のモジュールレベルキャッシュをテストごとに初期化する。"""
    index._cached_document = None
    yield
    index._cached_document = None


# ── 1. スロットリングが利用者に漏れないこと ────────────────


class TestThrottlingIsRetried:
    def test_Bedrockのスロットリングはリトライして回答を返す(self):
        """従来は 1 回の ThrottlingException で諦めてエラー文を返していた。"""
        client = MagicMock()
        client.invoke_model.side_effect = [throttling(), bedrock_response("20日です")]

        with patch.object(index, "_bedrock_client", client), patch("time.sleep"):
            answer = index.invoke_bedrock("規定本文", "有給は何日")

        assert answer == "20日です", "リトライせず諦めている"
        assert client.invoke_model.call_count == 2
        assert "しばらくしてから" not in answer

    def test_リトライを使い切ったらエラー文を返す(self):
        client = MagicMock()
        client.invoke_model.side_effect = throttling()

        cfg = wiring.RETRY_CONFIG
        with patch.object(index, "_bedrock_client", client), patch("time.sleep"):
            answer = index.invoke_bedrock("規定本文", "有給は何日")

        assert client.invoke_model.call_count == cfg.max_attempts
        assert "しばらくしてから" in answer

    def test_リトライ不能なエラーは即座に返す(self):
        denied = ClientError(
            {"Error": {"Code": "AccessDeniedException", "Message": "no"}},
            "InvokeModel",
        )
        client = MagicMock()
        client.invoke_model.side_effect = denied

        with patch.object(index, "_bedrock_client", client), patch("time.sleep"):
            answer = index.invoke_bedrock("規定本文", "質問")

        assert client.invoke_model.call_count == 1, "リトライすべきでない"
        assert "管理者にご連絡ください" in answer

    def test_S3のスロットリングもリトライされる(self):
        body = MagicMock()
        body.read.return_value = "規定本文".encode()
        client = MagicMock()
        client.get_object.side_effect = [throttling("GetObject"), {"Body": body}]

        with patch.object(index, "_s3_client", client), patch("time.sleep"):
            text = index.get_document_from_s3("b", "k.txt")

        assert text == "規定本文"
        assert client.get_object.call_count == 2


# ── 2. 質問文・回答文をログに残さないこと ──────────────────


class TestSensitiveTextIsNotLogged:
    def test_質問文がログに出ない(self, capsys, monkeypatch):
        secret = "山田太郎の休職について相談したい"
        monkeypatch.setattr(index, "logger", create_logger(level="debug"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: "回答本文")

        index.handler(api_event(secret), _Ctx())

        dumped = json.dumps(json_lines(capsys.readouterr()), ensure_ascii=False)
        assert "山田太郎" not in dumped, "質問文がログに残っている"
        assert "休職" not in dumped

    def test_質問の長さはフィールドで残る(self, capsys, monkeypatch):
        monkeypatch.setattr(index, "logger", create_logger(level="debug"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: "回答")

        index.handler(api_event("あいうえお"), _Ctx())

        entries = json_lines(capsys.readouterr())
        got = [e for e in entries if e.get("message") == "質問を受信しました"]
        assert got, "受信ログが出ていない"
        assert got[0]["question_length"] == 5

    def test_回答文がログに出ない(self, capsys, monkeypatch):
        answer = "就業規則第12条により、入社6か月後に10日付与されます"
        monkeypatch.setattr(index, "logger", create_logger(level="debug"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: answer)

        index.handler(api_event(), _Ctx())

        dumped = json.dumps(json_lines(capsys.readouterr()), ensure_ascii=False)
        assert "就業規則第12条" not in dumped, "回答文がログに残っている"

    def test_取り込みログのS3キーはフィールドで出る(self, capsys, monkeypatch):
        monkeypatch.setattr(ingestion_handler, "logger", create_logger(level="debug"))
        event = [
            {
                "body": json.dumps(
                    {
                        "Records": [
                            {
                                "s3": {
                                    "bucket": {"name": "docs"},
                                    "object": {"key": "就業規則_2026.png", "size": 10},
                                }
                            }
                        ]
                    }
                )
            }
        ]

        ingestion_handler.handler(event, _Ctx())

        entries = json_lines(capsys.readouterr())
        skipped = [e for e in entries if e.get("level") == "warn"]
        assert skipped
        assert skipped[0]["s3_key"] == "就業規則_2026.png"
        assert "就業規則" not in skipped[0]["message"]


# ── 3. リトライがログとメトリクスの両方に残ること ──────────


class TestRetryHooksAreWired:
    def test_retry_hooksは両方を呼ぶ(self):
        lines: list[str] = []
        emitted: list[str] = []
        log = create_logger(level="debug", sink=lambda line, level: lines.append(line))
        met = create_metrics("Test/NS", sink=emitted.append)

        wiring.retry_hooks("Bedrock.InvokeModel", log, met)(2, 0.25, throttling())

        entry = json.loads(lines[0])
        assert entry["operation"] == "Bedrock.InvokeModel"
        assert entry["attempt"] == 2
        met.flush()
        doc = json.loads(emitted[0])
        assert doc["retry_operation"] == "Bedrock.InvokeModel"
        assert doc["RetryAttempts"] == 1

    def test_metricsがNoneでもログだけで動く(self):
        lines: list[str] = []
        log = create_logger(level="debug", sink=lambda line, level: lines.append(line))

        wiring.retry_hooks("S3.GetObject", log, None)(1, 0.1, throttling())

        assert len(lines) == 1

    def test_Bedrockのリトライがログに残る(self):
        lines: list[str] = []
        log = create_logger(level="debug", sink=lambda line, level: lines.append(line))
        client = MagicMock()
        client.invoke_model.side_effect = [throttling(), bedrock_response()]

        with patch.object(index, "_bedrock_client", client), patch("time.sleep"):
            index.invoke_bedrock("本文", "質問", log=log)

        ops = [json.loads(x).get("operation") for x in lines]
        assert "Bedrock.InvokeModel" in ops, "リトライのログが出ていない＝未結線"

    def test_取り込みのS3リトライがログに残る(self):
        lines: list[str] = []
        log = create_logger(level="debug", sink=lambda line, level: lines.append(line))
        client = MagicMock()
        client.get_object.side_effect = [
            throttling("GetObject"),
            {"ContentLength": 10, "ContentType": "text/plain"},
        ]
        record = {
            "s3": {"bucket": {"name": "b"}, "object": {"key": "a.txt", "size": 10}}
        }

        with patch.object(ingestion_handler, "_s3_client", client), patch("time.sleep"):
            res = ingestion_handler._process_s3_record(record, log=log)

        assert res["status"] == "success"
        ops = [json.loads(x).get("operation") for x in lines]
        assert "S3.GetObject" in ops


# ── 4. EMF メトリクスが出力されること ──────────────────────


class TestMetricsAreEmitted:
    def test_ハンドラーがEMFを出す(self, capsys, monkeypatch):
        monkeypatch.setattr(index, "logger", create_logger(level="info"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: "回答")

        index.handler(api_event("あいう"), _Ctx())

        docs = [d for d in json_lines(capsys.readouterr()) if "_aws" in d]
        assert docs, "EMF が出ていない＝metrics.py が未結線"
        doc = docs[0]
        assert doc["QuestionCharacters"] == 3
        assert doc["AnswersReturned"] == 1
        assert doc["StatusCode"] == 200
        assert "HandlerDuration" in doc

    def test_不正なリクエストもEMFを出す(self, capsys, monkeypatch):
        monkeypatch.setattr(index, "logger", create_logger(level="info"))

        res = index.handler({"body": "{"}, _Ctx())

        assert res["statusCode"] == 400
        docs = [d for d in json_lines(capsys.readouterr()) if "_aws" in d]
        assert docs
        assert docs[0]["BadRequests"] == 1
        assert docs[0]["StatusCode"] == 400

    def test_取り込みハンドラーもEMFを出す(self, capsys, monkeypatch):
        monkeypatch.setattr(ingestion_handler, "logger", create_logger(level="info"))
        event = [{"body": json.dumps({"Records": []})}]

        ingestion_handler.handler(event, _Ctx())

        docs = [d for d in json_lines(capsys.readouterr()) if "_aws" in d]
        assert docs
        assert docs[0]["SqsRecords"] == 1

    def test_METRICS_ENABLED_falseで止まる(self, capsys, monkeypatch):
        monkeypatch.setenv("METRICS_ENABLED", "false")
        monkeypatch.setattr(index, "logger", create_logger(level="info"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: "回答")

        index.handler(api_event(), _Ctx())

        docs = [d for d in json_lines(capsys.readouterr()) if "_aws" in d]
        assert not docs

    def test_flush失敗でもレスポンスは返る(self, monkeypatch):
        monkeypatch.setattr(index, "logger", create_logger(level="silent"))
        monkeypatch.setattr(index, "S3_BUCKET_NAME", "")
        monkeypatch.setattr(index, "invoke_bedrock", lambda *a, **k: "回答")

        broken = create_metrics("Test/NS")
        monkeypatch.setattr(
            broken, "flush", MagicMock(side_effect=RuntimeError("boom"))
        )
        monkeypatch.setattr(index, "create_metrics_from_env", lambda **k: broken)

        res = index.handler(api_event(), _Ctx())

        assert res["statusCode"] == 200

    def test_S3キャッシュのヒットが記録される(self, capsys, monkeypatch):
        monkeypatch.setattr(index, "logger", create_logger(level="info"))
        index._cached_document = "キャッシュ済み本文"
        met = create_metrics("Test/NS")

        text = index.get_document_from_s3("b", "k.txt", metrics=met)

        assert text == "キャッシュ済み本文"
        lines = met.flush()
        assert json.loads(lines[0])["S3CacheHits"] == 1


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
