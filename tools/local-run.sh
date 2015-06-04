#!/bin/bash

godep go clean; godep go build

./websocket-proxy
