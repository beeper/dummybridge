FROM python:3.10-slim-bullseye

WORKDIR /opt/dummybridge
COPY . .
RUN pip install -e .
