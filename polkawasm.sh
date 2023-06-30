#!/usr/bin/env bash

docker build --tag polkawasm/tinygo:0.28.0 -f Dockerfile.polkawasm .
docker run --rm -it polkawasm/tinygo:0.28.0 bash