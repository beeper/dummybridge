import asyncio
import logging
import time
from collections import deque
from typing import Generator

import aiohttp
from faker import Faker
from mautrix.appservice import AppService
from mautrix.types import (
    BatchSendEvent,
    EventType,
    ImageInfo,
    MediaMessageEventContent,
    MessageType,
    TextMessageEventContent,
    UserID,
)

StateBridge = EventType.find("m.bridge", EventType.Class.STATE)

logger = logging.getLogger(__name__)


async def _download_image(image_url: str) -> bytes:
    async with aiohttp.ClientSession() as session:
        async with session.get(image_url) as response:
            content = await response.read()

    return content


async def _generate_random_image(size: int, category: str) -> bytes:
    """
    Get a random image from unsplash.
    """

    image_url = f"https://source.unsplash.com/random/{size}x{size}?{category}"
    return await _download_image(image_url)


class ContentGenerator:
    def __init__(self, user_prefix, user_domain):
        self.faker = Faker()
        self.user_prefix = user_prefix
        self.user_domain = user_domain

    def generate_userid(self):
        user_name = self.faker.user_name()
        return UserID(f"@{self.user_prefix}{user_name}:{self.user_domain}")

    async def download_and_upload_image(
        self,
        appservice: AppService,
        async_media_delay: int | None = None,
        image_size: int | None = None,
        image_category: str | None = None,
        image_url: str | None = None,
    ):
        if image_url:
            image_bytes = await _download_image(image_url)
        else:
            image_size = image_size or 128
            image_bytes = await _generate_random_image(size=image_size, category=image_category)

        if async_media_delay:
            mxc = await appservice.intent.unstable_create_mxc()

            async def _wait_then_upload():
                await asyncio.sleep(async_media_delay)
                await appservice.intent.upload_media(
                    image_bytes,
                    mime_type="image/png",
                    mxc=mxc,
                )

            asyncio.create_task(_wait_then_upload())
        else:
            mxc = await appservice.intent.upload_media(
                image_bytes,
                mime_type="image/png",
            )

        return mxc, image_bytes

    async def generate_image_message(
        self,
        appservice: AppService,
        async_media_delay: int | None = None,
        image_size: int | None = None,
        image_category: str | None = None,
        image_url: str | None = None,
    ):
        mxc, image_bytes = await self.download_and_upload_image(
            appservice=appservice,
            async_media_delay=async_media_delay,
            image_size=image_size,
            image_category=image_category,
            image_url=image_url,
        )

        return MediaMessageEventContent(
            msgtype=MessageType.IMAGE,
            url=mxc,
            body=mxc,
            info=ImageInfo(
                mimetype="image/png",
                size=len(image_bytes),
                width=image_size,
                height=image_size,
            ),
        )

    async def generate_text_message(
        self,
        appservice: AppService,
        room_id: str,
        message_text: str | None = None,
        reply_to_event_id: str | None = None,
    ):
        if reply_to_event_id:
            target_event = await appservice.intent.get_event(room_id, reply_to_event_id)

        msg = TextMessageEventContent(
            msgtype=MessageType.TEXT,
            body=message_text or self.faker.sentence(),
        )

        if reply_to_event_id:
            msg.set_reply(target_event)

        return msg

    async def generate_reaction_event(
        self,
        appservice: AppService,
        user_id: str,
        room_id: str,
        message_text: str,
        reply_to_event_id: str,
    ):
        return await appservice.intent.user(user_id).react(
            room_id,
            reply_to_event_id,
            message_text,
        )

    async def generate_content(
        self,
        appservice: AppService,
        owner: str,
        room_id: str = None,
        room_name: str | None = None,
        room_avatarurl: str | None = None,
        messages: int = 1,
        message_type: str = "text",
        message_text: str | None = None,
        users: int | None = None,
        user_ids: list[str] | None = None,
        user_displayname: str | None = None,
        user_avatarurl: str | None = None,
        async_media_delay: int | None = None,
        image_size: int | None = None,
        image_category: str | None = None,
        image_url: str | None = None,
        reply_to_event_id: str | None = None,
        bridge_name: str = "dummybridge",
        infinite_backfill: bool = False,
        infinite_backfill_delay: int = 0,
    ) -> tuple[str, list[str], list[str]]:
        # TODO: this function is a total mess now, probably be good to separate it into a few
        # sub-commands like?:
        # generate room
        # generate reply
        # generate reaction
        # etc, etc

        if room_id is None:
            if users is None:
                users = 1
            elif users == 0:
                raise ValueError("Must provide `room_id` when users is set to 0!")

        if reply_to_event_id is not None:
            if not room_id:
                raise ValueError("Must provide `room_id` when `reply_to_event_id` is set!")
            if messages > 1:
                raise ValueError("Must not specify >1 messages when `reply_to_event_id` is set!")

        if message_type == "reaction":
            if not reply_to_event_id:
                raise ValueError(
                    "Must specify `reply_to_event_id` when `message_type` is reaction!",
                )
            if not message_text:
                raise ValueError("Must specify `message_text` when `message_type` is reaction!")

        if user_ids:
            if users:
                raise ValueError("Must not specify `users` when `user_ids` set")
        elif users:
            user_ids = [self.generate_userid() for user in range(users)]
            for userid in user_ids:
                await appservice.intent.user(userid).ensure_registered()
                await appservice.intent.user(userid).set_displayname(
                    user_displayname or self.faker.name(),
                )
                if user_avatarurl:
                    user_avatar_mxc, _ = await self.download_and_upload_image(
                        appservice=appservice,
                        image_url=user_avatarurl,
                    )
                    await appservice.intent.user(userid).set_avatar_url(user_avatar_mxc)
        else:
            existing_user_ids = await appservice.intent.get_joined_members(room_id)
            user_ids = [
                userid
                for userid in existing_user_ids
                if userid.startswith(f"@{self.user_prefix}")
                and not userid.startswith(f"@{self.user_prefix}bot")
            ]

        def _user_id_generator() -> Generator[str, None, None]:
            while True:
                for userid in user_ids:
                    yield userid

        user_id_generator = _user_id_generator()

        if not room_id:
            initial_state = [
                {
                    "type": str(StateBridge),
                    "state_key": "i.am.a.bridge",
                    "content": {"protocol": {"id": bridge_name}},
                },
            ]

            room_id = await appservice.intent.create_room(
                name=room_name or self.faker.sentence(),
                initial_state=initial_state,
                creation_content={
                    "m.federate": False,
                },
            )
            if room_avatarurl:
                room_avatar_mxc, _ = await self.download_and_upload_image(
                    appservice=appservice,
                    image_url=room_avatarurl,
                )
                await appservice.intent.set_room_avatar(room_id, room_avatar_mxc)
            await appservice.intent.invite_user(room_id, owner)

        if message_type == "reaction":
            user_id = next(user_id_generator)
            react_event_id = await self.generate_reaction_event(
                appservice=appservice,
                user_id=user_id,
                room_id=room_id,
                message_text=message_text,
                reply_to_event_id=reply_to_event_id,
            )
            return room_id, [user_id], [react_event_id]

        if message_type == "text":

            async def generator():
                return await self.generate_text_message(
                    appservice=appservice,
                    room_id=room_id,
                    message_text=message_text,
                    reply_to_event_id=reply_to_event_id,
                )

        elif message_type == "image":

            async def generator():
                return await self.generate_image_message(
                    appservice=appservice,
                    async_media_delay=async_media_delay,
                    image_size=image_size,
                    image_category=image_category,
                )

        else:
            raise ValueError(f"Invalid `message_type`: {message_type}")

        message_events = [await generator() for _ in range(messages)]
        event_ids = []

        if infinite_backfill:
            event_id = await appservice.intent.send_message_event(
                room_id, EventType("fi.mau.dummy.pre_backfill", EventType.Class.MESSAGE), {}
            )
            await asyncio.sleep(infinite_backfill_delay)
            batch_send_events = [
                BatchSendEvent(
                    content=content,
                    type=EventType.ROOM_MESSAGE,
                    sender=next(user_id_generator),
                    timestamp=(
                        int(time.time() * 1000) - (1000 * (len(message_events) + 1)) + (1000 * i)
                    ),
                )
                for i, content in enumerate(message_events)
            ]
            logger.debug("Sending %d events %s", len(batch_send_events), str(batch_send_events))
            event_ids = (
                await appservice.intent.batch_send(
                    room_id,
                    event_id,
                    events=batch_send_events,
                    beeper_new_messages=True,
                )
            ).event_ids
        else:
            for content in message_events:
                event_ids.append(
                    await appservice.intent.user(next(user_id_generator)).send_message_event(
                        room_id,
                        EventType.ROOM_MESSAGE,
                        content,
                    ),
                )

        return room_id, user_ids, event_ids
