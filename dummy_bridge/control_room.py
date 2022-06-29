import asyncio
import json
import logging
from inspect import signature

from mautrix.appservice import AppService, IntentAPI
from mautrix.errors import MNotFound
from mautrix.types import (
    EventType,
    MessageType,
    PaginationDirection,
    TextMessageEventContent,
    UserID,
)

from .generate import ContentGenerator

logger = logging.getLogger(__name__)


HELP_TEXT = """
👋 Hello, {name} at your service! The following commands are available:

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
        use_websocket: bool,
        generator: ContentGenerator,
    ):
        self.appservice = appservice
        self.intent = appservice.intent
        self.owner = owner
        self.user_prefix = user_prefix
        self.use_websocket = use_websocket
        self.generator = generator

        self.command_map = {
            "help": self.send_help,
            "arguments": self.send_arguments,
            "audit": self.audit,
            "cleanup": self.cleanup,
            "generate": self.generate,
        }

    @property
    def name(self):
        return "DummyBridgeWS" if self.use_websocket else "DummyBridge"

    async def bootstrap(self):
        account_data = {
            "control_room_id": None,
        }

        try:
            account_data = await self.intent.get_account_data(self.name)
        except MNotFound:
            pass

        room_id = account_data.get("control_room_id")
        joined_members = []

        if room_id:
            logger.debug(f"Using existing control room {room_id}")
            joined_members = await self.intent.get_joined_members(room_id)
        else:
            logger.debug("Creating new control room")
            room_id = await self.intent.create_room(
                name=f"{self.name} Control",
                creation_content={
                    "m.federate": False,
                },
            )
            await self.intent.join_room(room_id)
            account_data["control_room_id"] = room_id
            await self.intent.set_account_data(self.name, account_data)

        if self.owner not in joined_members:
            logger.debug(f"Inviting owner {self.owner} to control room {room_id}")
            await self.intent.invite_user(room_id, self.owner)

        self.room_id = room_id
        return room_id

    async def on_event(self, event):
        if event.type is EventType.ROOM_MESSAGE:
            if event.content.msgtype == MessageType.FILE:
                if event.content.info.mimetype != "text/tab-separated-values":
                    await self.send_message(
                        f"⚠️ I don't understand file format: " f"{event.content.info.mimetype}",
                    )
                    return
                await self.generate_from_file(event.content.url)
                return

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
        await self.send_message(HELP_TEXT.format(name=self.name))

    async def send_arguments(self, content):
        sig = signature(ContentGenerator.generate_content)
        parameters = {
            k: p for k, p in sig.parameters.items() if k not in ("self", "appservice", "owner")
        }

        lines = ["Available arguments & defaults:"]
        for key, parameter in parameters.items():
            annotation = parameter.annotation
            if annotation is str:
                annotation = "str"
            lines.append(f"{key}: {annotation} = {parameter.default}")

        await self.send_message("\n".join(lines))

    async def audit(self, content):
        await self.send_message("Running audit...")

        found_dead_rooms = False

        lines = []
        room_ids = await self.intent.get_joined_rooms()
        for room_id in room_ids:
            joined_members = await self.intent.get_joined_members(room_id)
            bot_members = [
                member for member in joined_members if member.startswith(f"@{self.user_prefix}")
            ]
            real_member_count = len(joined_members) - len(bot_members)

            lines.append(
                f"Room: {room_id} has {len(joined_members)} members "
                f"({len(bot_members)} bots, "
                f"{real_member_count} real users)",
            )

            if not real_member_count:
                found_dead_rooms = True

        await self.send_message("\n".join(lines))
        if found_dead_rooms:
            await self.send_message(
                "Found rooms with no real users, run cleanup to remove them!",
            )

    async def cleanup(self, content):
        await self.send_message("Starting cleanup...")

        room_ids = await self.intent.get_joined_rooms()
        for room_id in room_ids:
            joined_members = await self.intent.get_joined_members(room_id)
            bot_members = [
                member for member in joined_members if member.startswith(f"@{self.user_prefix}")
            ]
            real_member_count = len(joined_members) - len(bot_members)

            if not real_member_count:
                for bot_member in bot_members:
                    await self.intent.user(bot_member).leave_room(room_id)
                await self.intent.leave_room(room_id)
                await self.send_message(f"🚫 Removed all room members & left: {room_id}")

        await self.send_message("✅ Cleanup complete!")

    async def generate(self, content):
        try:
            kwargs = json.loads(content.split(None, 1)[1])
        except (IndexError, json.JSONDecodeError):
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

        arguments = "\n".join([f"{key} = {value}" for key, value in kwargs.items()])
        await self.send_message(f"⏳ Generating with arguments:\n{arguments}")
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

    async def generate_from_file(self, mxc: str):
        try:
            await self._generate_from_file(mxc=mxc)
        except Exception as e:
            await self.send_message(f"💀 Error generating from file: {e}")
            raise
        else:
            await self.send_message("✅ File generation complete, enjoy!")

    async def _generate_from_file(self, mxc: str):
        await self.send_message("File received, starting work...")

        contents = await self.intent.download_media(mxc)
        contents = contents.decode()

        lines = contents.splitlines()
        headers = [h.lower().replace(" ", "-") for h in lines[0].split("\t")]
        messages = [dict(zip(headers, line.split("\t"))) for line in lines[1:]]

        # Row 1 is special and contains extras like room name / chat app
        room_id, _, _ = await self.generator.generate_content(
            appservice=self.appservice,
            owner=self.owner,
            messages=0,
            bridge_name=messages[0]["chat-app"],
            room_name=messages[0]["room-name"],
        )

        await self.send_message("🗂️ Created room...")

        contact_to_user_id = {}

        for message in messages:
            contact = message["contact"]
            if contact == "contact" or contact in contact_to_user_id:
                continue

            _, user_ids, _ = await self.generator.generate_content(
                appservice=self.appservice,
                owner=self.owner,
                messages=0,
                room_id=room_id,
                users=1,
                user_displayname=message["contact"],
                user_avatarurl=message["avatar-url"],
            )
            contact_to_user_id[contact] = user_ids[0]

        await self.send_message(f"🧑 Created {len(contact_to_user_id)} users...")

        message_event_ids = {}

        for i, message in enumerate(messages):
            if message["contact"] == "sender":
                await self.send_message(
                    "First message is from sender, that's you!\n"
                    "I'll wait here until you send a message in the new room",
                )
                while True:
                    try:
                        room_messages = await self.intent.get_messages(
                            room_id=room_id,
                            direction=PaginationDirection.BACKWARD,
                            from_token="",
                            filter_json={"types": [str(EventType.ROOM_MESSAGE)]},
                        )
                    except Exception as e:
                        if "not in response" not in f"{e}":
                            raise
                        continue

                    events = room_messages.events
                    assert len(events) == 1, "You sent too many messages!"
                    message_event_ids[message["message-id"]] = events[0].event_id
                    if room_messages:
                        break

                    await asyncio.sleep(10)
                continue

            generator_kwargs = {
                "room_id": room_id,
                "user_ids": [contact_to_user_id[message["contact"]]],
                "messages": 1,
                "delay": message.get("delay"),
            }

            message_type = message.get("message-type", "text")
            reply_to_message_id = message.get("reply-to-message-id")

            if message_type == "text":
                generator_kwargs["message_text"] = message["content"]
            elif message_type == "reaction":
                generator_kwargs["message_type"] = "reaction"
                generator_kwargs["message_text"] = message["content"]
                assert (
                    reply_to_message_id is not None
                ), "Cannot have reaction without reply-to-message-id set"
            elif message_type == "image":
                generator_kwargs["message_type"] = "image"
                generator_kwargs["image_url"] = message["content"]
                pass
            else:
                raise ValueError(f"Invalid message-type: {message_type}")

            if reply_to_message_id:
                generator_kwargs["reply_to_event_id"] = message_event_ids[reply_to_message_id]

            _, _, event_ids = await self.generator.generate_content(
                appservice=self.appservice,
                owner=self.owner,
                **generator_kwargs,
            )
            message_event_ids[message["message-id"]] = event_ids[0]

        await self.send_message(f"📩 Sent {len(message_event_ids)} messages...")
        await self.send_message("✅ File generation complete, enjoy!")
