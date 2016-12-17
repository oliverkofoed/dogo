package main

import "github.com/docker/docker-credential-helpers/credentials"

func getPlatformCredStore(store string) credentials.Helper {
	switch store {
	case "wincred":
		return wincred.Wincred{}
	default:
		return nil
	}
}
