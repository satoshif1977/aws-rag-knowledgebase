"""
aws-rag-knowledgebase: Streamlit Web UI
API Gateway 経由で Lambda を呼び出して回答を表示するデモアプリ
"""

import streamlit as st
import boto3
import json
import os

# ── ページ設定 ────────────────────────────────────────────────
st.set_page_config(
    page_title="社内規定 PDF Q&A",
    page_icon="📚",
    layout="centered",
)

# ── サイドバー設定 ────────────────────────────────────────────
with st.sidebar:
    st.header("設定")
    function_name = st.text_input(
        "Lambda 関数名",
        value=os.environ.get("LAMBDA_FUNCTION_NAME", "rag-kb-dev"),
    )
    aws_region = st.text_input(
        "AWS リージョン",
        value=os.environ.get("AWS_REGION", "ap-northeast-1"),
    )
    st.divider()
    st.markdown("### 使い方")
    st.markdown(
        """
1. S3 バケットにテキストファイルをアップロード
2. 質問を入力して送信
3. ドキュメントの内容に基づいて AI が回答

**S3 アップロード例:**
```
aws s3 cp knowledge.txt \\
  s3://{バケット名}/documents/knowledge.txt
```
"""
    )
    st.divider()
    st.caption("aws-rag-knowledgebase PoC | Powered by Amazon Bedrock")

# ── メイン画面 ────────────────────────────────────────────────
st.title("📚 社内規定 PDF Q&A")
st.caption("社内ドキュメントの内容について質問してください。")

# ── チャット履歴の初期化 ──────────────────────────────────────
if "messages" not in st.session_state:
    st.session_state.messages = []

# ── チャット履歴の表示 ────────────────────────────────────────
for message in st.session_state.messages:
    with st.chat_message(message["role"]):
        st.markdown(message["content"])
        if message.get("source"):
            source_label = "📂 S3 ドキュメント参照" if message["source"] == "s3_document" else "🌐 一般知識"
            st.caption(source_label)

# ── 質問入力 ──────────────────────────────────────────────────
if prompt := st.chat_input("質問を入力してください（例：有給休暇の申請手順は？）"):

    st.session_state.messages.append({"role": "user", "content": prompt})
    with st.chat_message("user"):
        st.markdown(prompt)

    with st.chat_message("assistant"):
        with st.spinner("ドキュメントを参照して回答を生成中..."):
            try:
                lambda_client = boto3.client("lambda", region_name=aws_region)
                payload = {"body": json.dumps({"question": prompt})}
                response = lambda_client.invoke(
                    FunctionName=function_name,
                    InvocationType="RequestResponse",
                    Payload=json.dumps(payload),
                )
                result = json.loads(response["Payload"].read())
                body = json.loads(result.get("body", "{}"))
                answer = body.get("answer", "回答を取得できませんでした。")
                source = body.get("source", "general")

            except Exception as e:
                answer = f"⚠️ エラーが発生しました: {str(e)}"
                source = None

        st.markdown(answer)
        if source:
            source_label = "📂 S3 ドキュメント参照" if source == "s3_document" else "🌐 一般知識"
            st.caption(source_label)

    st.session_state.messages.append({
        "role": "assistant",
        "content": answer,
        "source": source,
    })

# ── チャット履歴クリアボタン ──────────────────────────────────
if st.session_state.messages:
    if st.button("会話をクリア", type="secondary"):
        st.session_state.messages = []
        st.rerun()
