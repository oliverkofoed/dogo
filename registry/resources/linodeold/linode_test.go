package linodeold_test

import (
	"os"
	"strings"
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/linode"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestLinode(t *testing.T) {
	apikey, err := os.ReadFile("test_apikey.txt")
	if err != nil {
		t.Error(err)
		return
	}
	apikey = []byte(strings.TrimSpace(string(apikey)))

	l := &schema.ConsoleLogger{}
	group := &linode.LinodeGroup{
		DecommissionTag: "groupa",
		APIKey:          testmodule.MockTemplate(apikey),
	}

	box1 := &linode.Linode{
		Name:         "web23",
		Datacenter:   testmodule.MockTemplate("dallas"),
		Plan:         testmodule.MockTemplate("Linode 2048/1cores/2048mb/24gb/2000xfer"),
		Distribution: testmodule.MockTemplate("Ubuntu 16.04 LTS"),
		Disks:        testmodule.MockTemplate("swap:256"),
		Kernel:       testmodule.MockTemplate("Latest 64 bit (4.8.6-x86_64-linode78)"),
		PrivateIPs:   1,
		SSHPrivateKey: testmodule.MockFileTemplate([]byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAzgJ0Mz3EncZHIbUD3q+EWW8zPA8D8+Eu0zbdwzOGUKLsXST9
gavy0UhSgDRT86EmnYPd7s9Di8EkVRdy1qffdiKeQffZMad/gr8a8bplI7Ut0IkT
sis3Cfvlxpk/qR1I2WzqG632b15GukkcW2gzTTOKkj+AqECVEpnRkTEHHYU8ZkPs
SSNBdyn11oCylnpNAky5oN+LtUvQJvqoZrktY1GR7Tmq33NeQHk3JotLl7VyCa6c
4I1W2AhRfVfW3rVEru6iVVpp8epQYl9+saISZD+y2k2yi/XpTVVyhzO2rZKMt/xo
L4Om7VZTu2lkWKTPhWmwuzb8YhumEPEQiNrPMQIDAQABAoIBABKQogwkGt3lCm/9
MhYVVyYAIWveJosJ1gBux1laAVau+AIE3VucNUuq6tRm4tHnyeUUByIIR5wGkdGh
RVYW1sp8oCptvYL+Bz2vHyx9kbPAFhre34mE33bk3nYhRV1mKDR/3jEUYkrzAgiz
ofySzVy9slUvp9aBy21bs0kUVAHS4IDyz1qwtHkqE+wth8IzzbfBj/PIqATThwFr
5OiO4pOApL7rFitVviBh0Gc52lOg+o8EhYJ4oT46hLHcedyrrJFtt4506vKvxPLW
PLbkcgOXOgS9KPecZkdEibal9/dNeynU/2NAaRqrAl1+n2sxqCTDvtZBaDI1BbFq
9uCpIwkCgYEA9ADE0V+eyrgOb2edjbKRthfVL+iny9zDjBE/n/63eqTrTBy59iIW
1fYcgTyY652vwgbYo3tDvtwE+Gzs89tzlpdgs5IWsS8wYDDSOat+CQD8DAOIjVaA
sPm++TQO8fYSi0MV/yFFh+fNE/wkKy1IGxislDa59Ty4s0+USTTus58CgYEA2CN3
ovPq+C6+BkrCuWeGdUx1pLWBAyQPbOAdwpmaZkPZ/r8UxKwzSmMokzEyMwGYMCQq
TVtP2Q1yywwIPmwsYw5aTvuX7MruUNQMCDRMDIHQBa5SHlOna1DHPRwFiGk91wHw
rH2mZUgcO6abcRfliF4BhzwXJVTXNLVe2K6wCy8CgYA33KUyug2Eo7bKUpKDikpJ
whMQsNcZmSU7wAcs/gfLkE4+UqVQcGWB/qJwBAuOhb9jUGXwp5vO6lhI98cX3ToN
VALTmbKQRhlxLDw078ofDZamuXhdw1wbKFJMg1qYkpmUQHucuWVNxAfzd1pgeDF1
4qRAGndgadJvWty8Fd5ASQKBgHR7EMOR/oSH9ELB0ZVHtI/Mh+4fHwsJSQLc+Uzh
qPMKCBag9dlUEEQ7kidZMPuKFXGEXAPafPq1o7LHpj214Gn11zePoX2sk6idzmox
fPaUkv4sxvavEJ/mJanKSzULupb/5auf/6e/p++Bx224eiv2tY4jFTo6McynHhla
c2djAoGABXhA5tumj8Dr2nloiX/LRtNpbQ9t1NyGgdrlsxmTiSTM+JeLReifFM9m
bNP4v1aU3OLr0TLKYEkeZ44jOHtjUcq9Yx+dwAWHmVFC/2xZf92yceEofuVONB+X
+ki8w0SoynCrsNmyaeET77AUmNeAF+ksaByrIJ5A97Y42Jtl88A=
-----END RSA PRIVATE KEY-----`)),
		SSHPublicKey: testmodule.MockFileTemplate([]byte(`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDOAnQzPcSdxkchtQPer4RZbzM8DwPz4S7TNt3DM4ZQouxdJP2Bq/LRSFKANFPzoSadg93uz0OLwSRVF3LWp992Ip5B99kxp3+CvxrxumUjtS3QiROyKzcJ++XGmT+pHUjZbOobrfZvXka6SRxbaDNNM4qSP4CoQJUSmdGRMQcdhTxmQ+xJI0F3KfXWgLKWek0CTLmg34u1S9Am+qhmuS1jUZHtOarfc15AeTcmi0uXtXIJrpzgjVbYCFF9V9betUSu7qJVWmnx6lBiX36xohJkP7LaTbKL9elNVXKHM7atkoy3/Ggvg6btVlO7aWRYpM+FabC7NvxiG6YQ8RCI2s8x sample@bamble.com`)),
		RootPassword: testmodule.MockTemplate(""),
	}
	err = linode.Manager.Provision(group, box1, l)
	if err != nil {
		t.Error(err)
		return
	}
}
