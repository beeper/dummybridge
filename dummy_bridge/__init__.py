import asyncio
import json
import logging
import re
from urllib.parse import urlparse

import aiohttp
from mautrix.api import HTTPAPI
from mautrix.appservice import AppService
from mautrix.appservice.state_store import ASStateStore
from mautrix.client.api import ClientAPI
from mautrix.client.state_store.memory import MemoryStateStore
from mautrix.types import Event, UserID
from mautrix.util.bridge_state import BridgeState, BridgeStateEvent, GlobalBridgeState

from .control_room import ControlRoom
from .generate import ContentGenerator

WEBSOCKET_PING_INTERVAL = 5
WEBSOCKET_MAX_RECONNECT_DELAY = 60

logger = logging.getLogger(__name__)


class MemoryBridgeStateStore(ASStateStore, MemoryStateStore):
    def __init__(self) -> None:
        ASStateStore.__init__(self)
        MemoryStateStore.__init__(self)


class DummyBridge:
    control_room: ControlRoom
    control_room_id: str

    def __init__(
        self,
        homeserver: str,
        registration: dict[str, str],
        owner: UserID,
        host: str = "127.0.0.1",
        port: int = 5000,
        domain: str | None = None,
        use_websocket: bool = False,
    ):
        self.host = host
        self.port = port
        self.owner = owner
        self.use_websocket = use_websocket
        self.homeserver = homeserver
        self.registration = registration

        user_regex = registration["namespaces"]["users"][0]["regex"].replace("\\", "")
        matches = re.match(r"^@(.+)\.\+\:(.+)$", user_regex)
        self.user_prefix = matches.group(1)
        self.user_domain = matches.group(2)

        self.api = HTTPAPI(base_url=homeserver, token=registration["as_token"])

        if not domain:
            domain = urlparse(homeserver).netloc

        self.appservice = AppService(
            id=registration["id"],
            domain=domain or homeserver,
            server=homeserver,
            as_token=registration["as_token"],
            hs_token=registration["hs_token"],
            bot_localpart=registration["sender_localpart"],
            state_store=MemoryBridgeStateStore(),
        )
        self.appservice.matrix_event_handler(self.on_event)

    async def bootstrap(self):
        logger.debug("Bootstrap DummyBridge")

        # Populate the workaround hack for r0 -> v3 endpoint rewriting
        client_api = ClientAPI(api=self.api)
        await client_api.versions()

        await self.appservice.start(host=self.host, port=self.port)
        await self.appservice.intent.ensure_registered()

        generator = ContentGenerator(self.user_prefix, self.user_domain)

        self.control_room = ControlRoom(
            appservice=self.appservice,
            owner=self.owner,
            user_prefix=self.user_prefix,
            generator=generator,
        )
        self.control_room_id = await self.control_room.bootstrap()

        if self.use_websocket:
            asyncio.create_task(self.start_websocket_loop())

    async def on_event(self, event):
        if event.room_id == self.control_room_id:
            await self.control_room.on_event(event)
        else:
            logger.warning(
                "Received event for non control room: "
                f"roomId={event.room_id} eventId={event.event_id}",
            )

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
                "data": state_response.serialize(),
            }

            await ws.send_json(pong)
        else:
            logger.warning(f"Unknown websocket command: {command}")

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
