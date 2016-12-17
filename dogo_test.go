package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/oliverkofoed/dogo/registry"
)

func TestDogo(t *testing.T) {
	registry.GobRegister()
	start := time.Now()
	conf, errs := buildConfig("")
	_ = conf
	if errs != nil {
		printErrors(errs)
		return
	} else {
		fmt.Println("Parsing took", time.Since(start))
	}
	//dogoDeploy(conf, conf.Environments["dev"], false)
}
