package linode_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/linode"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestLinode(t *testing.T) {
	l := &schema.ConsoleLogger{}
	group := &linode.LinodeGroup{
		DecommissionTag: "groupa",
		APIKey:          testmodule.MockTemplate("RQOLQAi4CuIjVc2MCsLZTagOLEK3opF6ycXj4hI3bJ08hzptHJyHmmT7NaleckH3"), //TODO: do not check this fucker IN!!
	}

	box1 := &linode.Linode{
		Name:         "web23",
		Datacenter:   testmodule.MockTemplate("dallas"),
		Plan:         testmodule.MockTemplate("Linode 2048/1cores/2048mb/24gb/2000xfer"),
		Distribution: testmodule.MockTemplate("Ubuntu 16.04 LTS"),
		Disks:        testmodule.MockTemplate("swap:256"),
		Kernel:       testmodule.MockTemplate("Latest 64 bit (4.8.6-x86_64-linode78)"),
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
		//SSHPublicKey: testModule.Mock

	}
	err := linode.Manager.Provision(group, box1, l)
	if err != nil {
		t.Error(err)
		return
	}

	/*
		c := &client{apikey: "RQOLQAi4CuIjVc2MCsLZTagOLEK3opF6ycXj4hI3bJ08hzptHJyHmmT7NaleckH3"}

		node := LinodeServer{
			Datacenter:   testmodule.MockTemplate("dallas"),
			Plan:         testmodule.MockTemplate("Linode 2048"),
			Distribution: testmodule.MockTemplate("Ubuntu 16.04 LTS"),
			Disks:        testmodule.MockTemplate("swap:256"),
			Kernel:       testmodule.MockTemplate("Latest 64 bit (4.8.3-x86_64-linode76)"),
		}

		// load initial data in parallel.
		infoOnce.Do(func() {
			var wg sync.WaitGroup
			wg.Add(4)
			go func() {
				if d, err := c.availDatacenters(); err == nil {
					datacenters = d
				} else {
					initError = err
				}
				wg.Done()
			}()
			go func() {
				if p, err := c.availLinodeplans(); err == nil {
					plans = p
				} else {
					initError = err
				}
				wg.Done()
			}()
			go func() {
				if d, err := c.availDistributions(); err == nil {
					distributions = d
				} else {
					initError = err
				}
				wg.Done()
			}()
			go func() {
				if k, err := c.availKernels(); err == nil {
					kernels = k
				} else {
					initError = err
				}
				wg.Done()
			}()
			wg.Wait()
		})

		if initError != nil {
			panic(initError)
		}

		// find datacenter
		datacenterId := -1
		wantedDataCenter, err := node.Datacenter.Render(nil)
		if err != nil {
			panic(err)
		}
		for _, dc := range datacenters {
			if dc.ABBR == wantedDataCenter {
				datacenterId = dc.DATACENTERID
			}
		}
		if datacenterId == -1 {
			avaliableDataCenters := make([]string, 0, len(datacenters))
			for _, dc := range datacenters {
				avaliableDataCenters = append(avaliableDataCenters, dc.ABBR)
			}
			panic(fmt.Errorf("%v is not a valid datacenter. Avaliable data centers: [%v]", wantedDataCenter, strings.Join(avaliableDataCenters, ",")))
		}

		// find plan
		planID := -1
		wantedPlan, err := node.Plan.Render(nil)
		if err != nil {
			panic(err)
		}
		for _, p := range plans {
			if p.LABEL == wantedPlan {
				planID = p.PLANID
			}
		}
		if planID == -1 {
			avaliablePlans := make([]string, 0, len(plans))
			for _, p := range plans {
				avaliablePlans = append(avaliablePlans, p.LABEL)
			}
			panic(fmt.Errorf("%v is not a valid plan. Avaliable plans: [%v]", wantedPlan, strings.Join(avaliablePlans, ",")))
		}

		// find kernel
		kernelID := -1
		wantedKernel, err := node.Kernel.Render(nil)
		if err != nil {
			panic(err)
		}
		for _, k := range kernels {
			if k.LABEL == wantedKernel {
				kernelID = k.KERNELID
			}
		}
		if kernelID == -1 {
			avaliableKernels := make([]string, 0, len(kernels))
			for _, k := range kernels {
				avaliableKernels = append(avaliableKernels, k.LABEL)
			}
			panic(fmt.Errorf("%v is not a valid kernel. Avaliable kernels: [%v]", wantedKernel, strings.Join(avaliableKernels, ",")))
		}

		// find distribution
		var dist *distribution
		wantedDistribution, err := node.Distribution.Render(nil)
		if err != nil {
			panic(err)
		}
		for _, d := range distributions {
			if d.LABEL == wantedDistribution {
				dist = &d
			}
		}
		if dist == nil {
			avaliableDistributions := make([]string, 0, len(distributions))
			for _, d := range distributions {
				avaliableDistributions = append(avaliableDistributions, d.LABEL)
			}
			panic(fmt.Errorf("%v is not a valid distribution. Avaliable distributions: [%v]", wantedDistribution, strings.Join(avaliableDistributions, ",")))
		}

		// find disks
		disks := make([]disk, 0, 0)
		wantedDisks, err := node.Disks.Render(nil)
		if err != nil {
			panic(err)
		}
		for _, part := range strings.Split(wantedDisks, ",") {
			subparts := strings.Split(strings.TrimSpace(part), ":")
			if len(subparts) != 2 {
				panic(fmt.Sprintf("%v is not a valid disk declaration. Disks must be in the form type:size, with multiple disks seperated by comma. E.g.: 'ext:1024,swap:256'"))
			}

			diskType := subparts[0]
			diskSizeStr := subparts[1]
			if diskType != "ext4" && diskType != "ext3" && diskType != "swap" && diskType != "raw" {
				panic(fmt.Sprintf("%v is not a valid disk type. Valid disktypes: [ext4, ext3, swap, raw]"))
			}

			diskSize, err := strconv.Atoi(diskSizeStr)
			if err != nil {
				panic(fmt.Sprintf("%v is not a valid disk size: %v", diskSizeStr, err))
			}

			disks = append(disks, disk{
				label:          fmt.Sprintf("%v%v", diskType, len(disks)+1),
				distributionID: -1,
				diskType:       diskType,
				size:           diskSize,
			})
		}

		// calculate disk sizes
		totalHD := 24576 // TODO!
		diskSizeUsed := 0
		for _, disk := range disks {
			diskSizeUsed += disk.size
		}
		if totalHD-diskSizeUsed-dist.MINIMAGESIZE < 0 {
			panic(fmt.Sprintf("The size of the distribution (%v) + the combined size of other disks (%v) exceeds the capacity of the machine (%v)", dist.MINIMAGESIZE, diskSizeUsed, totalHD))
		}
		disks = append([]disk{disk{
			label:          dist.LABEL,
			distributionID: dist.DISTRIBUTIONID,
			size:           totalHD - diskSizeUsed,
		}}, disks...)

		disks[0].id = 6444980
		disks[1].id = 6444981
		*

					// create the linode
					linode, err := c.linodeCreate(datacenterId, planID)
					if err != nil {
						panic(err)
					}
					fmt.Println("CREATED: ", linode)

					// set the node label
					err := c.linodeUpdateLabel(linodeID, "something_web3")
					if err != nil {
						panic(err)
					}

				// create the disks.
				jobs := make([]createDiskJob, 0, 0)
				for _, disk := range disks {
					if disk.distributionID > 0 {
						if j, err := c.linodeDiskCreateFromDistribution(linodeID, disk.distributionID, disk.label, disk.size, "tester123", ""); err != nil {
							panic(err)
						} else {
							jobs = append(jobs, j)
						}
					} else {
						if j, err := c.linodeDiskCreate(linodeID, disk.label, disk.diskType, false, disk.size); err != nil {
							panic(err)
						} else {
							jobs = append(jobs, j)
						}

					}
				}


				// create configuration for the machine
				disklist := ""
				for i, disk := range disks {
					if i > 0 {
						disklist += ","
					}
					disklist += fmt.Sprintf("%v", disk.id)
				}
				conf, err := c.linodeConfigCreate(linodeID, kernelID, "config", "", 0, disklist, "paravirt", "default", 1, "", false, true, true, true, true, true)
				if err != nil {
					panic(err)
				}


			// create private ips for the machine
			if node.PrivateIPs<0 || node.PrivateIPs > 3 {
				panic( fmt.Errorf("You can't have %v private ips. Only 0-3.", node.PrivateIPs))
			}
			for i := 0; i < node.PrivateIPs; i++ {
				err = c.linodeIPAddPrivate(linodeID)
				if err != nil {
					panic(err)
				}
			}


	*/

	//linodeID := 2454393

	// give the machien a private ip

	// wait for jobs to finish
	// wait for machien to boot
	// save ips somewhere.
	// destroy machines (and disks)

	// boot the machine
	//err = c.linodeBoot(linodeID)
	//if err != nil {
	//panic(err)
	//}

	//_ = linodeID

	// boot!

	// lists configs
	// cache to disk.

	// list ips

	// List nodes

	// create a nodes
	// create node
	// format disks + ubuntu
	// ssh access with private key.

	// delete a node
	// something about disks?
}
