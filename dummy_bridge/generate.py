import asyncio
import hashlib
import random
from collections import deque
from string import ascii_letters

import aiohttp
from faker import Faker
from mautrix.appservice import AppService
from mautrix.types import (
    EventType,
    ImageInfo,
    MediaMessageEventContent,
    MessageType,
    TextMessageEventContent,
    UserID,
)


async def _generate_random_image(size):
    """
    Generate a random PNG... using Gravatar (why not).
    """

    rnd = "".join(random.choice(ascii_letters) for _ in range(24))
    md5 = hashlib.md5(rnd.encode()).hexdigest()
    image_url = f"https://gravatar.com/avatar/{md5}?d=identicon&s={size}"
    async with aiohttp.ClientSession() as session:
        async with session.get(image_url) as response:
            content = await response.read()

    return content


class ContentGenerator:
    def __init__(self, user_prefix, user_domain):
        self.faker = Faker()
        self.user_prefix = user_prefix
        self.user_domain = user_domain

    def generate_userid(self):
        user_name = self.faker.user_name()
        return UserID(f"@{self.user_prefix}{user_name}:{self.user_domain}")

    async def generate_image_message(
        self,
        appservice: AppService,
        async_media_delay: int | None = None,
        image_size: int | None = None,
    ):
        image_size = image_size or 128
        image_bytes = await _generate_random_image(size=image_size)

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

    async def generate_text_message(self):
        return TextMessageEventContent(
            msgtype=MessageType.TEXT,
            body=self.faker.sentence(),
        )

    async def generate_content(
        self,
        appservice: AppService,
        owner: str,
        room_id: str = None,
        messages: int = 1,
        message_type: str = "text",
        users: int | None = None,
        async_media_delay: int | None = None,
        image_size: int | None = None,
    ):
        if room_id is None and users is None:
            users = 1

        if users:
            userids = [self.generate_userid() for user in range(users)]
            for userid in userids:
                await appservice.intent.user(userid).ensure_registered()
        else:
            if not room_id:
                raise ValueError("Must provide `room_id` when users is set to 0!")

            existing_userids = await appservice.intent.get_joined_members(room_id)
            userids = [
                userid
                for userid in existing_userids
                if userid.startswith(f"@{self.user_prefix}")
                and not userid.startswith(f"@{self.user_prefix}bot")
            ]

        if not room_id:
            room_id = await appservice.intent.create_room(name=self.faker.sentence())
            await appservice.intent.invite_user(room_id, owner)

        userids = deque(userids)

        if message_type == "text":
            generator = self.generate_text_message
        elif message_type == "image":

            async def generator():
                return await self.generate_image_message(
                    appservice=appservice,
                    async_media_delay=async_media_delay,
                    image_size=image_size,
                )

        else:
            raise ValueError(f"Invalid `message_type`: {message_type}")

        messages = [await generator() for user in range(messages)]

        for message in messages:
            await appservice.intent.user(userids[0]).send_message_event(
                room_id,
                EventType.ROOM_MESSAGE,
                message,
            )
            userids.rotate()
