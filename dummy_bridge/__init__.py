import asyncio
import logging
import re
from urllib.parse import urlparse

from mautrix.api import HTTPAPI
from mautrix.appservice import AppService
from mautrix.appservice.state_store import ASStateStore
from mautrix.client.api import ClientAPI
from mautrix.client.state_store.memory import MemoryStateStore
from mautrix.types import UserID

from .control_room import ControlRoom
from .generate import ContentGenerator
from .message_send_status import MessageSendStatusHandler
from .websocket import WebSocketHandler

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
            ephemeral_events=True,
            as_token=registration["as_token"],
            hs_token=registration["hs_token"],
            bot_localpart=registration["sender_localpart"],
            state_store=MemoryBridgeStateStore(),
        )

        if self.use_websocket:
            self.websocket_handler = WebSocketHandler(
                homeserver=homeserver,
                registration=registration,
                on_event=self.on_event,
            )
        else:
            self.appservice.matrix_event_handler(self.on_event)

    async def bootstrap(self):
        logger.debug("Bootstrap DummyBridge")

        # Populate the workaround hack for r0 -> v3 endpoint rewriting
        self.client_api = ClientAPI(api=self.api)
        await self.client_api.versions()

        await self.appservice.start(host=self.host, port=self.port)
        await self.appservice.intent.ensure_registered()

        self.generator = ContentGenerator(self.user_prefix, self.user_domain)

        self.control_room = ControlRoom(
            appservice=self.appservice,
            owner=self.owner,
            user_prefix=self.user_prefix,
            use_websocket=self.use_websocket,
            generator=self.generator,
        )
        self.control_room_id = await self.control_room.bootstrap()

        self.message_send_status = MessageSendStatusHandler(
            appservice=self.appservice,
            owner=self.owner,
            generator=self.generator,
            client_api=self.client_api,
        )

        if self.use_websocket:
            logger.debug("Starting websocket loop...")
            asyncio.create_task(self.websocket_handler.start_websocket_loop())

    async def on_event(self, event):
        if event.type.is_ephemeral:
            logger.info(f"Received EDU: {event}")
            return

        if event.room_id == self.control_room_id:
            await self.control_room.on_event(event)
        else:
            logger.info(
                "Received event for non control room: "
                f"roomId={event.room_id} eventId={event.event_id}",
            )
            await self.message_send_status.handle_event(event)
