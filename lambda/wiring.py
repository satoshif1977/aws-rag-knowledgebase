"""
logger.py / metrics.py / retry.py を結線するための共通設定。

このモジュールが存在する理由:
  retry_logger()（logger.py）と retry_metrics()（metrics.py）は
  どちらも (試行回数, 待機秒数, 例外) を受ける同じ形をしている。
  両方を 1 つのフックにまとめる処理をどこに置くかが問題になる。

  - logger.py に置く → logger が metrics を import することになる
  - metrics.py に置く → その逆
  どちらも依存の向きを増やすので避けたい。

  そこで「束ねる役」をこのモジュールに切り出し、
  index.py（Q&A）と ingestion_handler.py（取り込み）の
  2 つのエントリーポイントから共有する。

TypeScript 版（client_ts/）・Go 版（lambda_go/）は
エントリーポイントが 1 つなので、同等の処理を main 側に直接書いている。
"""

from __future__ import annotations

import os
from collections.abc import Callable

from logger import StructuredLogger, retry_logger
from metrics import MetricsCollector, retry_metrics
from retry import RetryConfig

# ── リトライ設定 ───────────────────────────────────────────
# Bedrock は同時実行が増えると ThrottlingException を返す。
# S3 も SlowDown を返すことがある。どちらも数百ミリ秒待てば成功することが多い。
RETRY_CONFIG = RetryConfig(
    max_attempts=int(os.environ.get("RETRY_MAX_ATTEMPTS", "4")),
    base_delay=float(os.environ.get("RETRY_BASE_DELAY", "0.5")),
    max_delay=float(os.environ.get("RETRY_MAX_DELAY", "8.0")),
)


def retry_hooks(
    operation: str,
    log: StructuredLogger,
    metrics: MetricsCollector | None = None,
) -> Callable[[int, float, BaseException], None]:
    """retry_call(on_retry=...) に渡すフックを 1 つにまとめる。

    metrics が None のときはログだけに絞る
    （メトリクスの初期化に失敗してもリトライのログは残したいため）。
    """
    hooks = [retry_logger(log, operation)]
    if metrics is not None:
        hooks.append(retry_metrics(metrics, operation))

    def on_retry(attempt: int, delay_seconds: float, exc: BaseException) -> None:
        for hook in hooks:
            hook(attempt, delay_seconds, exc)

    return on_retry
