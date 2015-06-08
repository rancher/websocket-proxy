#!/bin/bash

godep go clean; godep go build

./websocket-proxy -jwt-public-key-file="/Users/cjellick/cattle-home/api.crt" -listen-address="localhost:8080"
