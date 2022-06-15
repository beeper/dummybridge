import asyncio

from mautrix.appservice.appservice import AppService
from mautrix.client.api.client import ClientAPI
from mautrix.types import EventType, RelatesTo, RelationType, UserID

from .generate import ContentGenerator
from .util import parse_args


HELP_TEXT = """
<ul>
<li>!help: show this help text!</li>
<li>!generate: generate messages<br>
The !generate command takes arguments in the form key=value, here are some examples:
<ul>
<li><code>!generate messages=10</code> &mdash;
Generate 10 messages (sent from all current users at random)</li>

<li><code>!generate messages=10 users=5</code> &mdash;
Generate 10 messages from 5 users (2 messages/user)</li>
</ul>
</li>
</ul>

<hr>

All other messages will be responded to with a <code>com.beeper.message_send_status</code>
event.<br>
To prevent a status event from being sent for a given message, include the text "nostatus" in the
message.<br>
To make the bridge send the status late, include the text "latestatus" in the message.<br>
By default, the message send status events will have success of <code>true</code>. However, if the
message contains the text "fail" then it will have success of <code>false</code>.<br>
If the message includes the text "noretry", then the status event will indicate that the failure
cannot be retried, and if the text "notcertain" is present, then the status event will indicate that
it is not certain that the event failed to bridge.<br>
The same rules apply for redactions, just put the text in the redaction reason.
""".strip()


class MessageSendStatusHandler:
    def __init__(
        self,
        appservice: AppService,
        owner: UserID,
        generator: ContentGenerator,
        client_api: ClientAPI,
    ):
        self.appservice = appservice
        self.owner = owner
        self.generator = generator
        self.client_api = client_api

    async def handle_event(self, event):
        if event.sender != self.owner:
            return

        if event.type not in (EventType.ROOM_MESSAGE, EventType.ROOM_REDACTION):
            return

        check_text = (
            event.content.body if event.type == EventType.ROOM_MESSAGE else event.content.reason
        ) or ""

        if event.type != EventType.ROOM_MESSAGE:
            if check_text.startswith("!generate"):
                try:
                    kwargs = parse_args(check_text.removeprefix("!generate"))
                except Exception as e:
                    await self.client_api.send_text(
                        event.room_id, f"Invalid arguments to generate. Type '!help' for usage. {e}"
                    )
                    return
                await self.generator.generate_content(
                    self.appservice, self.owner, room_id=event.room_id, **kwargs
                )
                return
            if check_text.startswith("!help"):
                await self.client_api.send_notice(event.room_id, html=HELP_TEXT)
                return

        if "nostatus" in check_text:
            return

        if "latestatus" in check_text:
            await asyncio.sleep(15)

        message_send_status_content = {
            "network": "dummybridge",
            "m.relates_to": RelatesTo(RelationType.REFERENCE, event.event_id).serialize(),
            "success": True,
        }
        if "fail" in check_text:
            no_retry = "noretry" in check_text
            not_certain = "notcertain" in check_text
            message_send_status_content.update(
                {
                    "success": False,
                    "reason": "m.foreign_network_error",
                    "error": "COM.BEEPER.DUMMY_FAIL",
                    "message": "'fail' was in the content body",
                    "can_retry": not no_retry,
                    "is_certain": not not_certain,
                }
            )

        await self.client_api.send_message_event(
            event.room_id,
            EventType("com.beeper.message_send_status", EventType.Class.MESSAGE),
            content=message_send_status_content,
        )
