import logging

from mautrix.appservice import AppService, IntentAPI
from mautrix.errors import MNotFound
from mautrix.types import EventType, UserID

from .generate import ContentGenerator


logger = logging.getLogger(__name__)


HELP_TEXT = """
Welcome to DummyBridge! The following commands are available:

help: show this help text!
generate: generate fake rooms, users and messages

Generate takes arguments in the form key=value, here are some examples:

Create a room with 10 messages (from one user):
    generate messages=10

Create a room with 10 messages from 5 users (2 messages/user):
    generate messages=10 users=5

Create 10 messages in an existing room (sent from all current users at random)
    generate roomID=!ABC:beeper.com messages=10
""".strip()


class ControlRoom:
    appservice: AppService
    intent: IntentAPI
    owner: UserID

    def __init__(self, appservice: AppService, owner: UserID, generator: ContentGenerator):
        self.appservice = appservice
        self.intent = appservice.intent
        self.owner = owner
        self.generator = generator

    async def bootstrap(self):
        account_data = {
            "control_room_id": None,
        }

        try:
            account_data = await self.intent.get_account_data("DummyBridge")
        except MNotFound:
            pass

        room_id = account_data.get("control_room_id")
        joined_members = []

        if room_id:
            logger.debug(f'Using existing control room {room_id}')
            joined_members = await self.intent.get_joined_members(room_id)
        else:
            logger.debug('Creating new control room')
            room_id = await self.intent.create_room(name="DummyBridge Control")
            await self.intent.join_room(room_id)
            account_data["control_room_id"] = room_id
            await self.intent.set_account_data("DummyBridge", account_data)

        if self.owner not in joined_members:
            logger.debug(f'Inviting owner {self.owner} to control room {room_id}')
            await self.intent.invite_user(room_id, self.owner)

        self.room_id = room_id
        return room_id

    async def on_event(self, event):
        if event.type is EventType.ROOM_MESSAGE:
            if event.content.body == 'help':
                await self.send_help()
            elif event.content.body.startswith('generate'):
                await self.generate(event.content.body)
            else:
                logger.warning(f"Unexpected control message: {event.content.body}")
        else:
            logger.warning(f"Unexpected control even type: {event.type}")

    async def send_message(self, content):
        await self.appservice.intent.send_message_event(
            self.room_id,
            EventType.ROOM_MESSAGE,
            {"msgtype": "m.notice", "body": content},
        )

    async def send_help(self):
        await self.send_message(HELP_TEXT)

    async def generate(self, content):
        bits = content.split()[1:]
        kwargs = {}

        for bit in bits:
            try:
                key, value = bit.split("=", 1)

                if key in ("messages", "users"):
                    value = int(value)
            except ValueError:
                await self.send_message(f"Invalid argument: {bit}")
                return
            else:
                kwargs[key] = value

        await self.send_message(f"Generating with arguments: {kwargs}")
        await self.generator.generate_content(
            appservice=self.appservice,
            owner=self.owner,
            **kwargs,
        )
