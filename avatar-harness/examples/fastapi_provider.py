"""Optional CPU provider example. Go Harness does not import this module.

Run with a Python environment containing fastapi and uvicorn.
It performs real text statistics and can describe an existing avatar URL.
It never invents ASR/TTS/Avatar inference.
"""
import os
from datetime import datetime, timezone

from fastapi import FastAPI
import uvicorn

app = FastAPI(title="Avatar Harness external provider example")


@app.get("/health")
def health():
    return {"status": "ok", "capabilities": ["text_stats", "avatar:describe"]}


@app.post("/invoke")
def invoke(body: dict):
    capability, payload = body.get("capability"), body.get("input", {})
    if capability == "text_stats":
        text = payload.get("text", "")
        if not isinstance(text, str):
            return {"status": "error", "output": {}, "error": "text must be a string"}
        return {"status": "ok", "output": {"characters": len(text), "lines": len(text.splitlines()) or 1, "utf8_bytes": len(text.encode()), "at": datetime.now(timezone.utc).isoformat()}}
    if capability == "avatar" and payload.get("operation") == "describe":
        asset, page = os.getenv("AVATAR_ASSET_URL"), os.getenv("AVATAR_RENDER_URL")
        if asset:
            return {"status": "ok", "output": {"renderer": {"kind": "scene", "asset_url": asset}}}
        if page:
            return {"status": "ok", "output": {"renderer": {"kind": "iframe", "url": page}}}
        return {"status": "deferred", "output": {}, "error": "Set AVATAR_ASSET_URL or AVATAR_RENDER_URL to an actual asset/page."}
    return {"status": "deferred", "output": {}, "error": "No inference model is loaded. Implement the real provider here."}


if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=9901)
