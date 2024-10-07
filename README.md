# DummyBridge

[![Go Reference](https://pkg.go.dev/badge/github.com/beeper/dummybridge.svg)](https://pkg.go.dev/github.com/beeper/dummybridge)
[![Go Report Card](https://goreportcard.com/badge/github.com/beeper/dummybridge)](https://goreportcard.com/report/github.com/beeper/dummybridge)
[![Build Docker](https://github.com/beeper/dummybridge/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/beeper/dummybridge/actions/workflows/docker.yaml)

DummyBridge is a Go-based bridge designed for testing purposes. It provides an echo bridge
functionality and supports various login flows, including password, cookies, local storage,
and display-and-wait.

## Features

- **Echo Bridge Functionality**: Acts as a simple echo bridge for testing.
- **Login Flows**: Supports multiple login flows such as password, cookies, local storage,
  and display-and-wait.
- **Automation**: Configurable automation options for management rooms, login, portals,
  and backfill.

## Docker Images

To install Docker, refer to the [Docker Engine install guide](https://docs.docker.com/engine/install/).

The DummyBridge project provides several Docker images:

- `ghcr.io/beeper/dummybridge:latest`: The main DummyBridge application.
- `ghcr.io/beeper/dummybridge/loginhelper:latest`: A helper service for managing login flows.
- `ghcr.io/beeper/dummybridge/go:latest`: A base image for Go applications.

To pull the latest version of the image, use the following:

```sh
docker pull ghcr.io/beeper/dummybridge:latest
```

## Configuration

DummyBridge can be configured using a YAML file. Below is an example configuration:

```yaml
automation:
  open_management_room: false # Open a management room for admins.
  login: false # Automatically log in admins.
  portals:
    count: 0 # Number of portals to create during startup.
    members: 0 # Number of members initially in each portal.
    messages: 0 # Number of messages initially sent to each portal.
  backfill:
    timelimit: 0s # Duration for the initial startup infinite backfill, e.g. 10s, 1m, 1h
```

## Running

To run DummyBridge using Docker, execute the following command:

```sh
docker run -d --name dummybridge -v /path/to/config.yaml:/etc/dummybridge/config.yaml ghcr.io/beeper/dummybridge:latest
```

Replace `/path/to/config.yaml` with the path to your configuration file.

## Login Flows

Once DummyBridge is launched, you can interact with it using various login flows:

1. **Password Login**:
   - Navigate to the login page and enter any username and password.
     These credentials are used as the ID.

2. **Cookies Login**:
   - Visit the `/pages/cookies.html` page and enter your username and password.
     These will be saved as cookies.

3. **Local Storage Login**:
   - Visit the `/pages/localstorage.html` page and enter your username and password.
     These will be saved in local storage.

4. **Display and Wait Login**:
   - A code will be generated, which you need to enter on the `/pages/daw_submit.html` page.

DummyBridge supports several commands for testing purposes, such as creating new rooms,
generating ghosts, and sending messages. These commands can be executed within the bridge's
management room.

## License

This project currently does not have a published license.
