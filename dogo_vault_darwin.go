package main

import (
	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/docker/docker-credential-helpers/osxkeychain"
)

func getPlatformCredStore(store string) credentials.Helper {
	switch store {
	case "osxkeychain":
		return osxkeychain.Osxkeychain{}
	default:
		return nil
	}
}
