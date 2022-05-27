import json
from typing import Any


def parse_args(argument_text: str) -> dict[str, Any]:
    arguments = {}
    for bit in argument_text.split():
        try:
            key, value = bit.split("=", 1)
        except ValueError:
            raise
        else:
            try:
                value = json.loads(value)
            except Exception:
                pass
            arguments[key] = value
    return arguments
