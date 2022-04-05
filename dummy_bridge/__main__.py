import asyncio
import click
import json
import logging

from . import DummyBridge


async def async_main(
    homeserver,
    owner,
    registration_file,
    port,
    host,
):
    logging.basicConfig(level=logging.WARNING)
    logging.getLogger("dummy_bridge").setLevel(level=logging.TRACE)

    with open(registration_file, "r") as f:
        registration = json.load(f)

    bridge = DummyBridge(
        homeserver_url=homeserver,
        registration=registration,
        owner=owner,
        listen_host=host,
        listen_port=port,
    )
    await bridge.bootstrap()
    await asyncio.Event().wait()


@click.command()
@click.argument("homeserver")
@click.argument("owner")
@click.argument("registration_file")
@click.option("--port", type=int, default=5000)
@click.option("--host", default="127.0.0.1")
def main(*args, **kwargs):
    asyncio.run(async_main(*args, **kwargs))


if __name__ == "__main__":
    main()
