package firewall_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/modules/firewall"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestFirewall(t *testing.T) {
	testmodule.TestModule(t, false, "../../../", firewall.Manager, []*firewall.Firewall{
		&firewall.Firewall{
			Protocol:  testmodule.MockTemplate("tcp"),
			Port:      80,
			From:      testmodule.MockTemplate("2.2.9.2,28.3.4.4"),
			Interface: testmodule.MockTemplate(""),
			Skip:      false,
		},
		&firewall.Firewall{
			Protocol:  testmodule.MockTemplate("tcp"),
			Port:      1211,
			From:      testmodule.MockTemplate("12.2.9.2,28.3.4.4,1::"),
			Interface: testmodule.MockTemplate(""),
			Skip:      false,
		},
		&firewall.Firewall{
			Protocol:  testmodule.MockTemplate("tcp"),
			Port:      22,
			From:      testmodule.MockTemplate(""),
			Interface: testmodule.MockTemplate(""),
			Skip:      false,
		},
	})
	return
}
