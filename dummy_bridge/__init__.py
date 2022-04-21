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

    async def on_event(self, event):
        if event.room_id == self.control_room_id:
            await self.control_room.on_event(event)
        else:
            logger.warning(
                "Received event for non control room: "
                f"roomId={event.room_id} eventId={event.event_id}",
            )
