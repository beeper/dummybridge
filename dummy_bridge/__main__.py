import asyncio
import json
import logging

import click

from . import DummyBridge


async def async_main(registration_file, **kwargs):
    logging.basicConfig(level=logging.WARNING)
    logging.getLogger("dummy_bridge").setLevel(level=logging.TRACE)

    with open(registration_file, "r") as f:
        registration = json.load(f)

    bridge = DummyBridge(registration=registration, **kwargs)
    await bridge.bootstrap()
    await asyncio.Event().wait()


@click.command()
@click.argument("homeserver")
@click.argument("owner")
@click.argument("registration_file")
@click.option("--port", type=int, default=5000)
@click.option("--host", default="127.0.0.1")
@click.option("--domain")
def main(*args, **kwargs):
    asyncio.run(async_main(*args, **kwargs))


if __name__ == "__main__":
    main()
