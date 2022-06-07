package qemu_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/qemu"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestQEMU(t *testing.T) {
	l := &schema.ConsoleLogger{}
	group := &qemu.QemuGroup{}

	// create box1
	boxUbuntux86_64 := &qemu.Qemu{
		System:  testmodule.MockTemplate("x86_64"),
		Name:    "ubuntu-x86_64",
		Image:   testmodule.MockTemplate("https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"),
		Memory:  2048,
		Storage: 1024 * 30,
		//ShowDisplay: true,
		CPUs: 4,
	}

	boxDebianArm := &qemu.Qemu{
		System:      testmodule.MockTemplate("aarch64"),
		Name:        "debian-aarch64",
		Image:       testmodule.MockTemplate("https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-generic-arm64.qcow2"),
		Memory:      2048,
		Storage:     1024 * 30,
		ShowDisplay: false,
		CPUs:        4,
	}
	boxUbuntuAaarch64 := &qemu.Qemu{
		System:      testmodule.MockTemplate("aarch64"),
		Name:        "ubuntu-aarch64",
		Image:       testmodule.MockTemplate("https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-arm64.img"),
		Memory:      2048,
		Storage:     1024 * 30,
		ShowDisplay: false,
		CPUs:        4,
	}
	_ = boxUbuntux86_64
	_ = boxUbuntuAaarch64
	_ = boxDebianArm

	err := qemu.Manager.Provision(group, boxUbuntuAaarch64, l)
	if err != nil {
		t.Error(err)
		return
	}
}
