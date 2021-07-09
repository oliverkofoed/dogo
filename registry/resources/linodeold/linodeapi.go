package linodeold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

type linodeStatus int

const (
	linodeStatusBeingCreated linodeStatus = -1
	linodeStatusBrandNew     linodeStatus = 0
	linodeStatusRunning      linodeStatus = 1
	linodeStatusPoweredOff   linodeStatus = 2
)

type createdLinode struct {
	LinodeID int
}

type distribution struct {
	IS64BIT             linodebool
	LABEL               string
	MINIMAGESIZE        int
	DISTRIBUTIONID      int
	REQUIRESPVOPSKERNEL linodebool
}

type createConfigResult struct {
	ConfigID int
}

type kernel struct {
	LABEL    string
	ISXEN    linodebool
	ISKVM    linodebool
	ISPVOPS  linodebool
	KERNELID int
}
type datacenter struct {
	DATACENTERID int
	LOCATION     string
	ABBR         string
}

type diskResponse struct {
	UPDATE_DT  string
	DISKID     int
	LABEL      string
	TYPE       string
	LINODEID   int
	ISREADONLY linodebool
	STATUS     int
	CREATE_DT  string
	SIZE       int
}

type createDiskJob struct {
	JobID  int
	DiskID int
}

type ipResponse struct {
	LINODEID    int
	ISPUBLIC    int
	IPADDRESS   string
	RDNS_NAME   string
	IPADDRESSID int
}

type plan struct {
	CORES  int
	PRICE  float64
	RAM    int
	XFER   int
	PLANID int
	LABEL  string
	DISK   int
	HOURLY float64
}

type linodeError struct {
	ERRORCODE    int
	ERRORMESSAGE string
}

type linode struct {
	TOTALXFER               int
	BACKUPSENABLED          linodebool
	WATCHDOG                linodebool
	LPM_DISPLAYGROUP        string
	ALERT_BWQUOTA_ENABLED   linodebool
	STATUS                  linodeStatus
	TOTALRAM                int
	ALERT_DISKIO_THRESHOLD  int
	BACKUPWINDOW            linodebool
	ALERT_BWOUT_ENABLED     linodebool
	ALERT_BWOUT_THRESHOLD   int
	LABEL                   string
	ALERT_CPU_ENABLED       linodebool
	ALERT_BWQUOTA_THRESHOLD int
	ALERT_BWIN_THRESHOLD    int
	BACKUPWEEKLYDAY         int
	DATACENTERID            int
	ALERT_CPU_THRESHOLD     int
	TOTALHD                 int
	ALERT_DISKIO_ENABLED    linodebool
	ALERT_BWIN_ENABLED      linodebool
	LINODEID                int
	CREATE_DT               string
	PLANID                  int
	DISTRIBUTIONVENDOR      string
	ISXEN                   linodebool
	ISKVM                   linodebool
}

type linodeJob struct {
	ENTERED_DT     string
	ACTION         string
	LABEL          string
	HOST_START_DT  string
	LINODEID       int
	HOST_FINISH_DT string
	HOST_MESSAGE   string
	JOBID          int
	HOST_SUCCESS   linodebool
}

type disk struct {
	id             int
	label          string
	distributionID int
	diskType       string
	size           int
}

type client struct {
	apikey string
}

func (c *client) req(output interface{}, action string, args ...interface{}) error {
	r := bytes.NewBuffer(nil)
	r.WriteString("https://api.linode.com/?api_key=")
	r.WriteString(url.QueryEscape(c.apikey))
	r.WriteString("&api_action=")
	r.WriteString(url.QueryEscape(action))
	for i := 0; i < len(args); i += 2 {
		r.WriteString("&")
		r.WriteString(fmt.Sprintf("%v", args[i]))
		r.WriteString("=")
		r.WriteString(url.QueryEscape(fmt.Sprintf("%v", args[i+1])))
	}

	response, err := http.Get(r.String())
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	root := struct {
		ERRORARRAY []linodeError
		DATA       json.RawMessage
	}{}
	err = json.Unmarshal(body, &root)
	if err != nil {
		return err
	}

	if len(root.ERRORARRAY) != 0 {
		return fmt.Errorf("Api(%v) error: %v", action, root.ERRORARRAY[0].ERRORMESSAGE)
	}

	err = json.Unmarshal(root.DATA, output)
	if err != nil {
		return fmt.Errorf("Linode API JSON error: %v. Trying to parse: %v", err, string(root.DATA))
	}
	return nil
}

func (c *client) linodeList() ([]linode, error) {
	res := make([]linode, 0)
	err := c.req(&res, "linode.list")
	return res, err
}

func (c *client) linodeShutdown(linodeID int) error {
	res := struct{}{}
	return c.req(&res, "linode.shutdown", "linodeid", linodeID)
}

func (c *client) linodeDelete(linodeID int) error {
	res := struct{}{}
	return c.req(&res, "linode.delete", "linodeid", linodeID)
}

func (c *client) linodeJobList(linodeID int, pendingOnly bool) ([]linodeJob, error) {
	res := make([]linodeJob, 0)
	err := c.req(&res, "linode.job.list", "linodeid", linodeID, "pendingonly", pendingOnly)
	return res, err
}

func (c *client) linodeBoot(linodeID int) error {
	res := struct{}{}
	return c.req(&res, "linode.boot", "linodeid", linodeID)
}

func (c *client) linodeIPAddPrivate(linodeID int) error {
	res := struct{}{}
	return c.req(&res, "linode.ip.addprivate", "linodeid", linodeID)
}

func (c *client) linodeDiskList(linodeID int) ([]diskResponse, error) {
	res := make([]diskResponse, 0)
	err := c.req(&res, "linode.disk.list", "linodeid", linodeID)
	return res, err
}

func (c *client) linodeDiskDelete(linodeID int, diskID int) error {
	res := struct{}{}
	return c.req(&res, "linode.disk.delete", "linodeid", linodeID, "diskid", diskID)
}

func (c *client) linodeDiskCreate(linodeID int, label string, diskType string, isReadOnly bool, size int) (createDiskJob, error) {
	res := createDiskJob{}
	err := c.req(&res, "linode.disk.create", "linodeid", linodeID, "label", label, "type", diskType, "isreadonly", isReadOnly, "size", size)
	return res, err
}

func (c *client) linodeDiskCreateFromDistribution(linodeID int, distributionID int, label string, size int, rootPass string, rootSSHKey string) (createDiskJob, error) {
	res := createDiskJob{}
	err := c.req(&res, "linode.disk.createfromdistribution", "linodeid", linodeID, "distributionid", distributionID, "label", label, "size", size, "rootPass", rootPass, "rootsshkey", rootSSHKey)
	return res, err
}

func (c *client) linodeConfigCreate(linodeID int, kernelID int, label string, comments string, ramlimit int, disklist string, virtMode string, runLevel string, rootDeviceNum int, rootDeviceCustom string, rootDeviceRO bool, helperDisableUpdateDB bool, helperDistro bool, helperDepmod bool, helperNetwork bool, devtmpfsAutomount bool) (*createConfigResult, error) {
	res := createConfigResult{}
	err := c.req(&res, "linode.config.create", "linodeid", linodeID, "kernelid", kernelID, "label", label, "comments", comments, "ramlimit", ramlimit, "disklist", disklist, "virt_mode", virtMode, "runlevel", runLevel, "rootDeviceNum", rootDeviceNum, "rootDeviceCustom", rootDeviceCustom, "rootdevicero", rootDeviceRO, "helper_disableupdatedb", helperDisableUpdateDB, "helper_distro", helperDistro, "helper_depmod", helperDepmod, "helper_network", helperNetwork, "devtmpfs_automount", devtmpfsAutomount)
	return &res, err
}

func (c *client) linodeUpdateLabel(linodeID int, label string) error {
	res := &createdLinode{}
	return c.req(&res, "linode.update", "linodeid", linodeID, "label", label)
}

func (c *client) linodeCreate(datacenterID int, planID int) (int, error) {
	res := createdLinode{}
	if err := c.req(&res, "linode.create", "datacenterid", datacenterID, "planid", planID); err != nil {
		return 0, err
	}
	return res.LinodeID, nil
}

func (c *client) lindeIPList(linodeID int) ([]ipResponse, error) {
	res := make([]ipResponse, 0)
	err := c.req(&res, "linode.ip.list", "linodeid", linodeID)
	return res, err
}

func (c *client) availDistributions() ([]distribution, error) {
	res := make([]distribution, 0)
	err := c.req(&res, "avail.distributions")
	return res, err
}

func (c *client) availKernels() ([]kernel, error) {
	res := make([]kernel, 0)
	err := c.req(&res, "avail.kernels")
	return res, err
}

func (c *client) availDatacenters() ([]datacenter, error) {
	res := make([]datacenter, 0)
	err := c.req(&res, "avail.datacenters")
	return res, err
}

func (c *client) availLinodeplans() ([]plan, error) {
	res := make([]plan, 0)
	err := c.req(&res, "avail.linodeplans")
	return res, err
}

type linodebool bool

func (b linodebool) UnmarshalJSON(data []byte) error {
	str := string(data)
	b = str == "1" || str == "true"
	return nil
}
