import logging
import json

from mautrix.appservice import AppService, IntentAPI
from mautrix.errors import MNotFound
from mautrix.types import EventType, UserID, MessageType, TextMessageEventContent

from .generate import ContentGenerator


logger = logging.getLogger(__name__)


HELP_TEXT = """
👋 Hello, DummyBridge at your service! The following commands are available:

help: show this help text!
generate: generate fake rooms, users and messages
arguments: show available arguments for the generate command

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

    def __init__(
        self,
        appservice: AppService,
        owner: UserID,
        user_prefix: str,
        generator: ContentGenerator,
    ):
        self.appservice = appservice
        self.intent = appservice.intent
        self.owner = owner
        self.user_prefix = user_prefix
        self.generator = generator

        self.command_map = {
            "help": self.send_help,
            "arguments": self.send_arguments,
            "audit": self.audit,
            "generate": self.generate,
        }

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
            for command_prefix, handler in self.command_map.items():
                if event.content.body.startswith(command_prefix):
                    await handler(event.content.body)
                    break
            else:
                logger.warning(f"Unexpected control message: {event.content.body}")
                await self.send_message(
                    f"⚠️ I don't understand command: {event.content.body}",
                )
        else:
            logger.warning(f"Unexpected control event type: {event.type}")
            await self.send_message(f"⚠️ I don't understand event type: {event.type}")

    async def send_message(self, content):
        await self.appservice.intent.send_message_event(
            self.room_id,
            EventType.ROOM_MESSAGE,
            TextMessageEventContent(
                msgtype=MessageType.NOTICE,
                body=content,
            ),
        )

    async def send_help(self, content):
        await self.send_message(HELP_TEXT)

    async def send_arguments(self, content):
        await self.send_message("Nah, not implemented that yet!")

    async def audit(self, content):
        self.send_message("Running audit...")

        lines = []
        room_ids = await self.intent.get_joined_rooms()
        for room_id in room_ids:
            joined_members = await self.intent.get_joined_members(room_id)
            bot_members = [
                member for member in joined_members
                if member.startswith(f"@{self.user_prefix}")
            ]
            lines.append(
                f"Room: {room_id} has {len(joined_members)} members "
                f"({len(bot_members)} bots, "
                f"{len(joined_members) - len(bot_members)} real users)"
            )

        await self.send_message("\n".join(lines))

    async def generate(self, content):
        bits = content.split()[1:]
        kwargs = {}

        for bit in bits:
            try:
                key, value = bit.split("=", 1)
            except ValueError:
                await self.send_message(f"Invalid argument: {bit}")
                return
            else:
                try:
                    value = json.loads(value)
                except Exception:
                    pass
                kwargs[key] = value

        await self.send_message(f"⏳ Generating with arguments: {kwargs}")
        try:
            await self.generator.generate_content(
                appservice=self.appservice,
                owner=self.owner,
                **kwargs,
            )
        except Exception as e:
            await self.send_message(f"💀 Error generating content: {e}")
            raise
        else:
            await self.send_message("✅ Generation complete, enjoy!")
