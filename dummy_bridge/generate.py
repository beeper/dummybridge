import asyncio
from collections import deque

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

StateBridge = EventType.find("m.bridge", EventType.Class.STATE)


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
    ):
        mxc, image_bytes = await self.download_and_upload_image(
            appservice=appservice,
            async_media_delay=async_media_delay,
            image_size=image_size,
            image_category=image_category,
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
        await appservice.intent.user(user_id).react(room_id, reply_to_event_id, message_text)

    async def generate_content(
        self,
        appservice: AppService,
        owner: str,
        room_id: str = None,
        room_name: str | None = None,
        messages: int = 1,
        message_type: str = "text",
        message_text: str | None = None,
        users: int | None = None,
        user_displayname: str | None = None,
        user_avatarurl: str | None = None,
        async_media_delay: int | None = None,
        image_size: int | None = None,
        image_category: str | None = None,
        reply_to_event_id: str | None = None,
        bridge_name: str = "dummybridge",
    ) -> None:
        # TODO: this function is a total mess now, probably be good to separate it into a few
        # sub-commands like?:
        # generate room
        # generate reply
        # generate reaction
        # etc, etc

        if room_id is None:
            if users is None:
                users = 1
            else:
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

        if users:
            userids = [self.generate_userid() for user in range(users)]
            for userid in userids:
                avatar_mxc, _ = await self.download_and_upload_image(
                    appservice=appservice,
                    image_url=user_avatarurl,
                )
                await appservice.intent.user(userid).ensure_registered()
                await appservice.intent.user(userid).set_displayname(
                    user_displayname or self.faker.name(),
                )
                await appservice.intent.user(userid).set_avatar_url(avatar_mxc)
        else:
            existing_userids = await appservice.intent.get_joined_members(room_id)
            userids = [
                userid
                for userid in existing_userids
                if userid.startswith(f"@{self.user_prefix}")
                and not userid.startswith(f"@{self.user_prefix}bot")
            ]

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
            )
            await appservice.intent.invite_user(room_id, owner)

        userids = deque(userids)

        if message_type == "reaction":
            await self.generate_reaction_event(
                appservice=appservice,
                user_id=userids[0],
                room_id=room_id,
                message_text=message_text,
                reply_to_event_id=reply_to_event_id,
            )
            return

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

        messages = [await generator() for user in range(messages)]

        for message in messages:
            await appservice.intent.user(userids[0]).send_message_event(
                room_id,
                EventType.ROOM_MESSAGE,
                message,
            )
            userids.rotate()
