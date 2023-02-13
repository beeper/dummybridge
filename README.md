# DummyBridge

`DummyBridge` is a simple Matrix bridge based on [`mautrix-python`](https://github.com/mautrix/python/) that can be used to generate fake rooms, users and messages for testing purposes.

**Example execution**

```
python -m dummy_bridge --domain beeper.local http://localhost:8009 @user:localhost registration.yaml
```
where:

* `beeper.local` is the domain that the bridge will run on,
* `http://localhost:8009` is the URL of hungryserv, and
* `@user:localhost` is the owner of the appservice.
