import asyncio
import logging

from mautrix.appservice.appservice import AppService
from mautrix.client.api.client import ClientAPI
from mautrix.types import EventType, RelatesTo, RelationType, UserID

from .generate import ContentGenerator
from .util import parse_args
from enum import Enum, auto
from typing import Tuple, Optional

logger = logging.getLogger(__name__)

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
event.<br><br>
To prevent a status event from being sent for a given message, include the text "nostatus" or "❌"
in the message.<br><br>
To make the bridge send the status late, include the text "latestatus" or "⏲️" in the message.<br><br>
By default, the message send status events will have success of <code>true</code>. However, if the
message contains the text "fail" or "🔥" then it will have success of <code>false</code>.<br><br>
If the message includes the text "noretry", then the status event will indicate that the failure
cannot be retried, and if the text "notcertain" is present, then the status event will indicate that
it is not certain that the event failed to bridge.<br><br>
The same rules apply for redactions (just put the text in the redaction reason) and reactions (just
react with the corresponding emoji). <br><br>
If "next" is included in the message, than whatever would have been applied to the current message
will instead be deferred and apply to the next message. For example, sending "next nostatus" will
get a successful status, but the next message sent will not receive a status, no matter what the
message is. This can be useful for testing redactions and retries.
""".strip()

class Action(Enum):
    SUCCESS = auto()
    FAIL = auto()
    NO_STATUS = auto()
    LATE = auto()
    GENERATE = auto()
    HELP = auto()

def action_from_checktext(check_text: str) -> Tuple[Action, bool, bool]:
    action = Action.SUCCESS
    no_retry = "noretry" in check_text
    not_certain = "notcertain" in check_text
    if "nostatus" in check_text or "❌" in check_text:
        action = Action.NO_STATUS
    if "latestatus" in check_text or "⏲️" in check_text:
        action = Action.LATE
    if "fail" in check_text or "🔥" in check_text:
        action = Action.FAIL
    if check_text.startswith("!generate"):
        action = Action.GENERATE
    if check_text.startswith("!help"):
        action = Action.HELP
    return action, no_retry, not_certain

def next_action_from_checktext(check_text: str) -> Optional[Action]:
    if "next" in check_text:
        action, _, _ = action_from_checktext(check_text)
        if action != Action.SUCCESS or "success" in check_text:
            return action
    return None

def check_text_from_event(event) -> str:
    check_text = ""
    if event.type == EventType.ROOM_MESSAGE:
        check_text = event.content.body or ""
    elif event.type == EventType.ROOM_REDACTION:
        check_text = event.content.reason or ""
    elif event.type == EventType.REACTION:
        check_text = event.content._relates_to.key or ""
    return check_text


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
        self.next_action: Optional[Action] = None

    async def handle_event(self, event):
        if event.sender != self.owner:
            return
        if event.type not in (EventType.ROOM_MESSAGE, EventType.ROOM_REDACTION, EventType.REACTION):
            return

        check_text = check_text_from_event(event)
        action, no_retry, not_certain = action_from_checktext(check_text)

        # Override next action if last action was a "next" action
        action_overridden = False
        if self.next_action:
            logger.debug(f"Performing action from override {self.next_action.name}")
            action_overridden = True
            action = self.next_action
        self.next_action = next_action_from_checktext(check_text)
        if self.next_action:
            logger.debug(f"Will override next action with {self.next_action.name}")
            await self.client_api.send_notice(event.room_id, text=f"Next message will have action {self.next_action.name}")
            action = Action.SUCCESS

        if action == Action.GENERATE:
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
        elif action == Action.HELP:
            await self.client_api.send_notice(event.room_id, html=HELP_TEXT)
        elif action == Action.NO_STATUS:
            return
        elif action == Action.LATE:
            await asyncio.sleep(15)

        message_send_status_content = {
            "network": "dummybridge",
            "m.relates_to": RelatesTo(RelationType.REFERENCE, event.event_id).serialize(),
            "status": "SUCCESS",
        }

        if action == Action.FAIL:
            no_retry = "noretry" in check_text

            message_send_status_content.update(
                {
                    "status": "FAIL_PERMANENT" if no_retry else "FAIL_RETRIABLE",
                    "reason": "m.foreign_network_error",
                    "error": "COM.BEEPER.DUMMY_FAIL" if not action_overridden else "COM.BEEPER.DUMMY_NEXT_FAIL",
                    "message": "'fail' was in the content body" if not action_overridden else "last message contained 'next fail'",
                }
            )

        await self.client_api.send_message_event(
            event.room_id,
            EventType("com.beeper.message_send_status", EventType.Class.MESSAGE),
            content=message_send_status_content,
        )
