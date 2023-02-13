import asyncio
import logging
import time

from mautrix.appservice.appservice import AppService
from mautrix.client.api.client import ClientAPI
from mautrix.types import EventType, RelatesTo, RelationType, UserID
from mautrix.util import background_task
from mautrix.util.message_send_checkpoint import (
    MessageSendCheckpoint,
    MessageSendCheckpointReportedBy,
    MessageSendCheckpointStatus,
    MessageSendCheckpointStep,
)

from .generate import ContentGenerator
from .util import parse_args
from enum import Enum, auto

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

<hr>

The bridge will also send message send checkpoints for all messages, redactions, and
reactions.<br><br>
By default, it will send successful <code>BRIDGE</code>, <code>DECRYPTED</code>, and
<code>REMOTE</code> checkpoints, however you can stop it from sending these by including the text
"nobridge" (or "🌉"), "nodecrypted" (or "🔐"), or "noremote" ("🤷") in the message. Note that
"nobridge" implies "nodecrypted" and "noremote", and "nodecrypted" implies "noremote".<br><br>
""".strip()


class MSSAction(Enum):
    SUCCESS = auto()
    FAIL = auto()
    NO_STATUS = auto()
    LATE = auto()
    GENERATE = auto()
    HELP = auto()


class CheckpointAction(Enum):
    SUCCESS = auto()
    NO_BRIDGE = auto()
    NO_DECRYPTED = auto()
    NO_REMOTE = auto()


def action_from_checktext(check_text: str) -> tuple[MSSAction, CheckpointAction, bool, bool]:
    mss_action = MSSAction.SUCCESS
    checkpoint_action = CheckpointAction.SUCCESS
    no_retry = "noretry" in check_text
    not_certain = "notcertain" in check_text
    if "nostatus" in check_text or "❌" in check_text:
        mss_action = MSSAction.NO_STATUS
    if "latestatus" in check_text or "⏲️" in check_text:
        mss_action = MSSAction.LATE
    if "fail" in check_text or "🔥" in check_text:
        mss_action = MSSAction.FAIL
    if check_text.startswith("!generate"):
        mss_action = MSSAction.GENERATE
    if check_text.startswith("!help"):
        mss_action = MSSAction.HELP
    if "nobridge" in check_text or "🌉" in check_text:
        checkpoint_action = CheckpointAction.NO_BRIDGE
    if "nodecrypted" in check_text or "🔐" in check_text:
        checkpoint_action = CheckpointAction.NO_DECRYPTED
    if "noremote" in check_text or "🤷" in check_text:
        checkpoint_action = CheckpointAction.NO_REMOTE
    return mss_action, checkpoint_action, no_retry, not_certain


def next_action_from_checktext(check_text: str) -> tuple[MSSAction | None, CheckpointAction | None]:
    if "next" in check_text:
        mss_action, checkpoint_action, _, _ = action_from_checktext(check_text)
        if (
            mss_action != MSSAction.SUCCESS
            or checkpoint_action != CheckpointAction.SUCCESS
            or "success" in check_text
        ):
            return mss_action, checkpoint_action
    return None, None


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
        checkpoint_endpoint: str,
    ):
        self.appservice = appservice
        self.owner = owner
        self.generator = generator
        self.client_api = client_api
        self.next_mss_action: MSSAction | None = None
        self.next_checkpoint_action: CheckpointAction | None = None
        self.checkpoint_endpoint: str = checkpoint_endpoint

    async def _send_checkpoint(
        self,
        evt,
        step: MessageSendCheckpointStep,
        status: MessageSendCheckpointStatus,
        err=None,
        retry_num: int | None = None,
    ):
        checkpoint = MessageSendCheckpoint(
            event_id=evt.event_id,
            room_id=evt.room_id,
            step=step,
            timestamp=int(time.time() * 1000),
            status=status,
            reported_by=MessageSendCheckpointReportedBy.BRIDGE,
            event_type=evt.type,
            message_type=evt.content.msgtype if evt.type == EventType.ROOM_MESSAGE else None,
            info=str(err) if err else None,
            retry_num=retry_num,
        )
        background_task.create(
            checkpoint.send(self.checkpoint_endpoint, self.appservice.as_token, logger)
        )

    async def handle_event(self, event):
        if event.sender != self.owner:
            return
        if event.type not in (EventType.ROOM_MESSAGE, EventType.ROOM_REDACTION, EventType.REACTION):
            return

        check_text = check_text_from_event(event)
        mss_action, checkpoint_action, no_retry, not_certain = action_from_checktext(check_text)

        if checkpoint_action != CheckpointAction.NO_BRIDGE:
            await self._send_checkpoint(
                event, MessageSendCheckpointStep.BRIDGE, MessageSendCheckpointStatus.SUCCESS
            )

        # We don't actually have encryption on the bridge, so this is just a mock.
        if checkpoint_action not in (CheckpointAction.NO_BRIDGE, CheckpointAction.NO_DECRYPTED):
            await self._send_checkpoint(
                event, MessageSendCheckpointStep.DECRYPTED, MessageSendCheckpointStatus.SUCCESS
            )

        # Override next action if last action was a "next" action
        action_overridden = False
        if self.next_mss_action:
            logger.debug(f"Performing action from override {self.next_mss_action.name}")
            action_overridden = True
            mss_action = self.next_mss_action
        self.next_mss_action, self.next_checkpoint_action = next_action_from_checktext(check_text)
        if self.next_mss_action:
            logger.debug(f"Will override next action with {self.next_mss_action.name}")
            await self.client_api.send_notice(
                event.room_id, text=f"Next message will have action {self.next_mss_action.name}"
            )
            mss_action = MSSAction.SUCCESS

        if mss_action == MSSAction.GENERATE:
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
        elif mss_action == MSSAction.HELP:
            await self.client_api.send_notice(event.room_id, html=HELP_TEXT)
        elif mss_action == MSSAction.NO_STATUS:
            return
        elif mss_action == MSSAction.LATE:
            await asyncio.sleep(15)

        if checkpoint_action not in (
            CheckpointAction.NO_BRIDGE,
            CheckpointAction.NO_DECRYPTED,
            CheckpointAction.NO_REMOTE,
        ):
            await self._send_checkpoint(
                event, MessageSendCheckpointStep.REMOTE, MessageSendCheckpointStatus.SUCCESS
            )

        message_send_status_content = {
            "network": "dummybridge",
            "m.relates_to": RelatesTo(RelationType.REFERENCE, event.event_id).serialize(),
            "status": "SUCCESS",
        }

        if mss_action == MSSAction.FAIL:
            no_retry = "noretry" in check_text

            message_send_status_content.update(
                {
                    "status": "FAIL_PERMANENT" if no_retry else "FAIL_RETRIABLE",
                    "reason": "m.foreign_network_error",
                    "error": (
                        "COM.BEEPER.DUMMY_FAIL"
                        if not action_overridden
                        else "COM.BEEPER.DUMMY_NEXT_FAIL"
                    ),
                    "message": (
                        "'fail' was in the content body"
                        if not action_overridden
                        else "last message contained 'next fail'"
                    ),
                }
            )

        await self.client_api.send_message_event(
            event.room_id,
            EventType("com.beeper.message_send_status", EventType.Class.MESSAGE),
            content=message_send_status_content,
        )
