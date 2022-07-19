import asyncio
import logging

from mautrix.appservice.appservice import AppService
from mautrix.client.api.client import ClientAPI
from mautrix.types import EventType, RelatesTo, RelationType, UserID
from mautrix.types.event.generic import Event

from .generate import ContentGenerator
from .util import parse_args
from enum import Enum, auto
from typing import Tuple, Optional, NamedTuple

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
event.<br>
To prevent a status event from being sent for a given message, include the text "nostatus" or "❌"
in the message.<br>
To make the bridge send the status late, include the text "latestatus" or "⏲️" in the message.<br>
By default, the message send status events will have success of <code>true</code>. However, if the
message contains the text "fail" or "🔥" then it will have success of <code>false</code>.<br>
If the message includes the text "noretry", then the status event will indicate that the failure
cannot be retried, and if the text "notcertain" is present, then the status event will indicate that
it is not certain that the event failed to bridge.<br>
The same rules apply for redactions (just put the text in the redaction reason) and reactions (just
react with the corresponding emoji).
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

def redaction_action_from_checktext(check_text: str) -> Optional[Action]:
    if "failredaction" in check_text:
        return Action.FAIL
    elif "lateredaction" in check_text:
        return Action.LATE
    elif "nostatusredaction" in check_text:
        return Action.NO_STATUS
    else:
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
        self.next_redaction_action: Optional[Action] = None

    async def handle_event(self, event):
        if event.sender != self.owner:
            return
        if event.type not in (EventType.ROOM_MESSAGE, EventType.ROOM_REDACTION, EventType.REACTION):
            return

        check_text = check_text_from_event(event)

        next_action, no_retry, not_certain = action_from_checktext(check_text)
        # Override next action if this is a redaction and the last action was a redaction action
        if event.type == EventType.ROOM_REDACTION and self.next_redaction_action:
            logger.debug("Performing REDACTION action")
            next_action = self.next_redaction_action
        self.next_redaction_action = redaction_action_from_checktext(check_text)
        if self.next_redaction_action:
            logger.debug("Will override action if next event is REDACTION")
            next_action = Action.SUCCESS

        if next_action == Action.GENERATE:
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
        elif next_action == Action.HELP:
            await self.client_api.send_notice(event.room_id, html=HELP_TEXT)
            return
        elif next_action == Action.NO_STATUS:
            return
        elif next_action == Action.LATE:
            await asyncio.sleep(15)

        message_send_status_content = {
            "network": "dummybridge",
            "m.relates_to": RelatesTo(RelationType.REFERENCE, event.event_id).serialize(),
            "success": True,
        }

        if next_action == Action.FAIL:
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
