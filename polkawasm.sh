#!/usr/bin/env bash

docker build --tag tinygo/polkawasm:0.31.0-dev -f Dockerfile.polkawasm .
docker run --rm -it tinygo/polkawasm:0.31.0-dev bash