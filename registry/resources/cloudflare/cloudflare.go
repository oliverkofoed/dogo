package cloudflare

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	cloudflarego "github.com/cloudflare/cloudflare-go"
	"github.com/oliverkofoed/dogo/schema"
)

type Cloudflare struct {
	APIToken        schema.Template `required:"true" description:"The api token for cloudflare"`
	ZoneID          schema.Template `required:"true" description:"The zoneid for the records"`
	DecommissionTag string          `required:"false" description:"Assign a tag to all servers. The tag will be used to decommission servers that have that tag, but aren't in the environment any longer."`
}

type DNS struct {
	Type    schema.Template `required:"false" default:"A" description:"which kind of record it is"`
	Name    schema.Template `required:"true" description:"the name of the record (domain)"`
	Content schema.Template `required:"true" description:"the contents of the record. usually an ip address"`
	TTL     schema.Template `required:"false" default:"1" description:"the ttl of the record"`
	Proxy   bool            `description:"Should cloudflare proxy requests to this name"`
}

var numberRegex = regexp.MustCompile(`\[([0-9]+)\-([0-9]+)\]`)

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "cloudflare",
	ResourcePrototype: &DNS{},
	GroupPrototype:    &Cloudflare{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		ctx := context.Background()
		g := group.(*Cloudflare)
		d := resource.(*DNS)

		// generate records
		typeID, err := d.Type.Render(nil)
		if err != nil {
			return err
		}
		name, err := d.Name.Render(nil)
		if err != nil {
			return err
		}
		content, err := d.Content.Render(nil)
		if err != nil {
			return err
		}
		ttl, err := d.TTL.Render(nil)
		if err != nil {
			return err
		}
		ttlInt, err := strconv.ParseInt(ttl, 10, 64)
		if err != nil {
			return fmt.Errorf("could not parse '%v' to integer TTL", ttl)
		}

		names := make([]string, 0)
		for _, n := range strings.Split(name, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				match := numberRegex.FindAllStringSubmatch(n, -1)
				if len(match) == 1 {
					start, err := strconv.ParseInt(match[0][1], 10, 64)
					if err != nil {
						return err
					}
					end, err := strconv.ParseInt(match[0][2], 10, 64)
					if err != nil {
						return err
					}
					if start >= end {
						return fmt.Errorf("Cloudflare name '%v', the start numeric generator must be less than the end. E.g; '[0-100]'", n)
					}

					for i := start; i < end; i++ {
						names = append(names, numberRegex.ReplaceAllString(n, fmt.Sprintf("%v", i)))
					}
				} else {
					names = append(names, n)
				}
			}
		}

		contents := make([]string, 0)
		for _, c := range strings.Split(content, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				contents = append(contents, c)
			}
		}

		targetRecords := make(map[string][]string)
		cptr := 0
		for _, name := range names {
			content := contents[cptr%len(contents)]
			cptr += 1

			arr, found := targetRecords[name]
			if !found {
				arr = make([]string, 0)
			}
			targetRecords[name] = append(arr, content)
		}

		nameptr := 0
		for i := cptr; i < len(contents); i++ {
			content := contents[i]
			name := names[nameptr%len(names)]
			nameptr += 1

			arr, found := targetRecords[name]
			if !found {
				arr = make([]string, 0)
			}
			targetRecords[name] = append(arr, content)
		}

		// get api and records for zone
		api, zoneID, records, err := getApiAndRecords(ctx, g)
		if err != nil {
			return err
		}

		for targetName, targets := range targetRecords {
			for _, targetContent := range targets {
				var foundRecord *cloudflarego.DNSRecord
				for _, record := range records[targetName] {
					if record.Name == targetName && record.Content == targetContent {
						foundRecord = &record
						break
					}
				}
				if foundRecord != nil {
					if foundRecord.Type != typeID || (foundRecord.Proxied != nil && *foundRecord.Proxied != d.Proxy) || foundRecord.TTL != int(ttlInt) {
						l.Logf("Updating record: %v (%v/%v) with type:%v, proxied:%v, ttl: %v", foundRecord.ID, foundRecord.Name, foundRecord.Content, typeID, d.Proxy, ttlInt)
						// udpdate record
						err := api.UpdateDNSRecord(ctx, zoneID, foundRecord.ID, cloudflarego.DNSRecord{
							Type:    typeID,
							Proxied: &d.Proxy,
							TTL:     int(ttlInt),
						})
						if err != nil {
							return fmt.Errorf("Could not update record#%v with type:%v, proxied:%v, ttl: %v. Error: %v", foundRecord.ID, typeID, d.Proxy, ttlInt, err)
						}
					}

				} else {
					// create dns record
					l.Logf("Creating record with type:%v, name:%v, content:%v, proxied:%v, ttl:%v", typeID, targetName, targetContent, d.Proxy, ttlInt)
					_, err := api.CreateDNSRecord(ctx, zoneID, cloudflarego.DNSRecord{
						Type:    typeID,
						Name:    targetName,
						Content: targetContent,
						Proxied: &d.Proxy,
						TTL:     int(ttlInt),
					})
					if err != nil {
						return fmt.Errorf("Could not create record with type:%v, name:%v, content:%v, proxied:%v, ttl:%v. Error: %v", typeID, targetName, targetContent, d.Proxy, ttlInt, err)
					}
				}
			}
		}

		return nil
	},
}

func getApiAndRecords(ctx context.Context, group interface{}) (*cloudflarego.API, string, map[string][]cloudflarego.DNSRecord, error) {
	g := group.(*Cloudflare)

	// get the api key.
	apiToken, err := g.APIToken.Render(nil)
	if err != nil {
		return nil, "", nil, err
	}

	// get the api key.
	zoneID, err := g.ZoneID.Render(nil)
	if err != nil {
		return nil, "", nil, err
	}

	// Construct a new API object
	api, err := cloudflarego.NewWithAPIToken(string([]byte(strings.TrimSpace(string(apiToken)))))
	if err != nil {
		return nil, "", nil, err
	}

	// get all records
	records, err := api.DNSRecords(ctx, zoneID, cloudflarego.DNSRecord{})
	if err != nil {
		return nil, "", nil, err
	}

	recordMap := make(map[string][]cloudflarego.DNSRecord)
	for _, record := range records {
		arr, found := recordMap[record.Name]
		if !found {
			arr = make([]cloudflarego.DNSRecord, 0)
		}
		recordMap[record.Name] = append(arr, record)
	}

	return api, zoneID, recordMap, nil
}
