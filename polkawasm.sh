#!/usr/bin/env bash

docker build --tag polkawasm/tinygo:0.28.1 -f Dockerfile.polkawasm .
docker run --rm -it polkawasm/tinygo:0.28.1 bash