#!/bin/bash

# build an agent and get its hash
pushd agent > /dev/null
go build -o .testagent
HASH=$(md5 -q .testagent)
rm .testagent
popd > /dev/null

# save hash in go code
sed -i -e "s/\".*\"/\"$HASH\"/g" testmodule/agenthash.go
rm -f testmodule/agenthash.go-e

# output new hash
echo $HASH