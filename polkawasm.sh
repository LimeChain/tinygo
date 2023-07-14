#!/usr/bin/env bash

docker build --tag tinygo/polkawasm:0.30.0 -f Dockerfile.polkawasm .
docker run --rm -it tinygo/polkawasm:0.30.0 bash