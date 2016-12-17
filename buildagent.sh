#!/bin/bash

# change version
sed -i -e "s/\".*\"/\"$(date +%s)\"/g" version/version.go
rm -f version/version.go-e

# into agent folder
pushd agent > /dev/null

# build Linux Build
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/agent.linux . &

# build macOS Build
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o .build/agent.darwin . &

# wait for builds to complete
wait

# back to project root
popd > /dev/null

# convert agents to gocode for embedding
go-bindata -o agents.go agent/.build