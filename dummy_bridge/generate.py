from collections import deque
from typing import Optional

from faker import Faker
from mautrix.appservice import AppService
from mautrix.types import EventType, UserID


class ContentGenerator:
    def __init__(self, user_prefix, user_domain):
        self.faker = Faker()
        self.user_prefix = user_prefix
        self.user_domain = user_domain

    def generate_userid(self):
        user_name = self.faker.user_name()
        return UserID(f"@{self.user_prefix}{user_name}:{self.user_domain}")

    def generate_message(self):
        return self.faker.sentence()

    async def generate_content(
        self,
        appservice: AppService,
        owner: str,
        room_id: str = None,
        messages: int = 1,
        users: Optional[int] = None,
    ):
        if room_id is None and users is None:
            users = 1

        if users:
            userids = [self.generate_userid() for user in range(users)]
            for userid in userids:
                await appservice.intent.user(userid).ensure_registered()

        if not room_id:
            room_id = await appservice.intent.create_room(name=self.faker.sentence())
            await appservice.intent.invite_user(room_id, owner)

        userids = deque(userids)
        messages = [self.generate_message() for user in range(messages)]

        for message in messages:
            await appservice.intent.user(userids[0]).send_message_event(
                room_id,
                EventType.ROOM_MESSAGE,
                {"msgtype": "m.text", "body": message},
            )
            userids.rotate()
