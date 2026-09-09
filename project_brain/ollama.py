from __future__ import annotations

import json
from urllib import request


class OllamaError(RuntimeError):
    pass


class OllamaClient:
    def __init__(self, base_url: str, model: str, timeout: int = 120):
        self.base_url = base_url.rstrip("/")
        self.model = model
        self.timeout = timeout

    def generate(self, prompt: str, *, system: str | None = None, num_ctx: int = 32768) -> str:
        payload = {
            "model": self.model,
            "prompt": prompt,
            "stream": False,
            "options": {"num_ctx": num_ctx, "temperature": 0},
        }
        if system:
            payload["system"] = system
        req = request.Request(
            f"{self.base_url}/api/generate",
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with request.urlopen(req, timeout=self.timeout) as response:
                data = json.loads(response.read().decode("utf-8"))
        except Exception as exc:  # network boundary
            raise OllamaError(f"Ollama request failed: {exc}") from exc
        if "response" not in data:
            raise OllamaError(f"Unexpected Ollama response: {data}")
        return data["response"].strip()

    def summarize(self, path: str, content: str, *, num_ctx: int = 32768) -> str:
        system = (
            "You are the Project Brain context worker. Produce a compact factual capsule for another coding agent. "
            "Do not invent behavior. Preserve public interfaces, invariants, dependencies, side effects and tests."
        )
        prompt = f"FILE: {path}\n\n{content}\n\nReturn concise Markdown, preferably under 500 words."
        return self.generate(prompt, system=system, num_ctx=num_ctx)
