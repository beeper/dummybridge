import asyncio
import json
import logging
from typing import Callable

import aiohttp
from mautrix.types import Event
from mautrix.util.bridge_state import BridgeState, BridgeStateEvent, GlobalBridgeState

WEBSOCKET_PING_INTERVAL = 5
WEBSOCKET_MAX_RECONNECT_DELAY = 60

logger = logging.getLogger(__name__)


class WebSocketHandler:
    def __init__(
        self,
        homeserver: str,
        registration: dict[str, str],
        on_event: Callable,
    ):
        self.homeserver = homeserver
        self.registration = registration
        self.on_event = on_event

    async def handle_websocket_message(self, ws, message):
        data = json.loads(message.data)
        command = data.get("command")

        if command == "transaction":
            for event in data["events"]:
                await self.on_event(Event.deserialize(event))
        elif command == "ping":
            state_response = GlobalBridgeState(
                remote_states={},
                bridge_state=BridgeState(state_event=BridgeStateEvent.UNCONFIGURED),
            )

            pong = {
                "id": data["id"],
                "command": "response",
                "data": {
                    "state": state_response.serialize(),
                    "echo": data["data"],
                },
            }

            logger.debug(f"Sending ping response: {pong}")
            await ws.send_json(pong)
        else:
            logger.warning(f"Unknown websocket command: {command}")
            await ws.send_json(
                {
                    "id": data["id"],
                    "command": "error",
                    "data": {
                        "code": "UNKNOWN_COMMAND",
                        "message": f"Unknown command {command}",
                    },
                }
            )

    async def read_websocket_messages(self, ws):
        async for message in ws:
            try:
                await self.handle_websocket_message(ws, message)
            except Exception as e:
                logger.critical(f"Fatal error handling websocket message: {message}")
                logger.exception(e)

    async def start_websocket_loop(self):
        read_messages_task = None
        delay = 1

        while True:
            async with aiohttp.ClientSession() as session:
                try:
                    async with session.ws_connect(
                        f"{self.homeserver}/_matrix/client/unstable/fi.mau.as_sync",
                        headers={
                            "Authorization": f"Bearer {self.registration['as_token']}",
                            "X-Mautrix-Process-ID": "DummyBridge",
                            "X-Mautrix-Websocket-Version": "3",
                        },
                    ) as ws:
                        delay = 1
                        read_messages_task = asyncio.create_task(self.read_websocket_messages(ws))

                        while True:
                            await asyncio.sleep(WEBSOCKET_PING_INTERVAL)
                            await ws.ping()
                except Exception as e:
                    logger.critical(
                        f"Fatal error with websocket connection, reconnecting in {delay}s...",
                    )
                    logger.exception(e)
                    if read_messages_task:
                        read_messages_task.cancel()

                    await asyncio.sleep(delay)

                    delay *= 2
                    delay = min(delay, WEBSOCKET_MAX_RECONNECT_DELAY)
