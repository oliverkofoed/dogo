#!/bin/bash

# change version
sed -i -e "s/\".*\"/\"$(date +%s)\"/g" version/version.go
rm -f version/version.go-e

# into agent folder
pushd agent > /dev/null

# build Linux Build
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/agent.linux.amd64 . &

# build Linux Build
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o .build/agent.linux.arm64 . &

# build macOS Build
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o .build/agent.darwin.amd64 . &

# build macOS Build
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o .build/agent.darwin.arm64 . &

# wait for builds to complete
wait

# back to project root
popd > /dev/null

# convert agents to gocode for embedding
# go-bindata -o agents.go agent/.build