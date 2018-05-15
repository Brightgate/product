/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bufio"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"bg/cl_common/daemonutils"
	"bg/common"

	"go.uber.org/zap"

	"cloud.google.com/go/storage"
	"golang.org/x/net/context"
)

const (
	googleStorage = "https://storage.googleapis.com/"
	gcpBucket     = "bg-blocklist-a198e4a0-5823-4d16-8950-ad34b32ace1c"
	ipBlacklist   = "ip_blacklist"
	dnsBlacklist  = "dns_blacklist"
	dnsWhitelist  = "dns_whitelist"

	phishPrefix = "http://data.phishtank.com/data/"
	phishKey    = "bd4ea8a80e25662e85f349c84bf300995ef013528c8201455edaeccf7426ec5e"
	phishList   = "online-valid.csv"

	etPrefix            = "https://rules.emergingthreats.net/open/suricata-4.0/rules/"
	botccRules          = "botcc.rules"
	compromisedIPsRules = "compromised-ips.txt"

	gcpEnv = "GOOGLE_APPLICATION_CREDENTIALS"
)

var (
	credFile  = flag.String("creds", "", "GCP credentials file")
	whitelist = flag.String("wl", "", "DNS source whitelist file")
	helpFlag  = flag.Bool("h", false, "help")

	log  *zap.Logger
	slog *zap.SugaredLogger
)

type blocklist map[string]string

type source struct {
	name   string
	file   string
	url    string
	parser func(*source, blocklist, *os.File) int
}

type dataset struct {
	name    string
	stem    string
	sources *[]source
}

var ipSources = []source{
	{
		name:   "Emerging Threats Compromised IPs",
		file:   "et.compromised_ips.txt",
		url:    etPrefix + compromisedIPsRules,
		parser: parseIPList,
	},
	{
		name:   "Emerging Threats abuse.ch Botnet C&C List",
		file:   "et.botnet.rules",
		url:    etPrefix + botccRules,
		parser: parseBotnets,
	},
}

var dnsSources = []source{
	{
		name:   "Malware Domains DNS blacklist",
		file:   "malwaredomains.zones",
		url:    "http://dns-bh.sagadc.org/malwaredomains.zones",
		parser: parseMalwaredomainsList,
	},
	{
		name:   "Phishtank DNS blacklist",
		file:   "online-valid.csv",
		url:    phishPrefix + phishKey + "/" + phishList,
		parser: parsePhishtank,
	},
	{
		name:   "Brightgate DNS blacklist",
		file:   "bg_dns.csv",
		url:    googleStorage + gcpBucket + "/bg_dns_blacklist.csv",
		parser: parseBrightgate,
	},
}

var whitelistSources = []source{}

var datasets = []dataset{
	{
		name:    "DNS blacklist",
		stem:    dnsBlacklist,
		sources: &dnsSources,
	},
	{
		name:    "DNS whitelist",
		stem:    dnsWhitelist,
		sources: &whitelistSources,
	},
	{
		name:    "IP blacklist",
		stem:    ipBlacklist,
		sources: &ipSources,
	},
}

// Add an address to a blocklist map.  If the address is already in the
// map, append the new reason to any existing reasons.
func addBlock(list blocklist, addr string, reason string) bool {
	var new bool

	r, ok := list[addr]
	if !ok {
		r = reason
		new = true
	} else {
		r += ("|" + reason)
	}
	list[addr] = r

	return new
}

/***************************************************************************
 *
 * Build DNS Blocklist
 *
 ***************************************************************************/

//
// Process the list from malwaredomains.com.  There is one line for each domain,
// each of which looks like this:
//
// zone "la21jeju.or.kr"  {type master; file "/etc/namedb/blockeddomain.hosts";};
//
func parseMalwaredomainsList(s *source, list blocklist, file *os.File) int {
	var cnt int

	ruleRE := regexp.MustCompile(`^zone "(.*)"  {`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		m := ruleRE.FindStringSubmatch(line)
		if len(m) == 2 {
			if addBlock(list, m[1], "MDL") {
				cnt++
			}
		}
	}

	return cnt
}

//
// Process the list from phishtank.com.  For each blocked domain, there is a
// single line that looks like this:
//
// 5471614,https://associacaoempresarial.com.br,
//     http://www.phishtank.com/phish_detail.php?phish_id=5471614,
//     2018-02-08T22:11:11+00:00,yes,2018-02-08T22:17:08+00:00,yes,Craigslist
func parsePhishtank(s *source, list blocklist, file *os.File) int {
	var cnt int

	phishID := `(\d+),`
	protocols := `(?:http|https|ftp)://`
	domainChars := `((?:\w|-|\.)+)`
	terminators := `(?:/|,|:|\?|#)`

	ruleRE := regexp.MustCompile(`^` + phishID + `"?` + protocols +
		domainChars + terminators + `.*`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		m := ruleRE.FindStringSubmatch(line)
		if len(m) == 3 {
			if addBlock(list, m[2], "phish-"+m[1]) {
				cnt++
			}
		}
	}
	return cnt
}

// Process the Brightgate-created list.
func parseBrightgate(s *source, list blocklist, file *os.File) int {
	var cnt int

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line[0] != '#' {
			name := line
			if idx := strings.Index(name, ","); idx > 0 {
				name = name[:idx]
			}
			if addBlock(list, name, "brightgate") {
				cnt++
			}
		}
	}
	return cnt
}

/***************************************************************************
 *
 * Build IP Blocklist
 *
 ***************************************************************************/

//
// Process Emerging Threat's list of compromised IPs.  Each line of the file
// contains just an a.b.c.d IP address
//
func parseIPList(s *source, list blocklist, file *os.File) int {
	var cnt int

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if ip := net.ParseIP(line); ip != nil {
			if addBlock(list, line, s.name) {
				cnt++
			}
		}
	}

	return cnt
}

//
// Process Emerging Threat's list of anti-botnet rules.  Each rule contains a
// list of IP addresses and quite a bit of detail.  A sample rule looks like:
//
// alert ip $HOME_NET any ->
//     [50.18.108.100,50.18.21.241,50.56.86.206,50.7.225.170,50.7.9.2,50.96.3.81,
//      50.97.249.115,5.101.78.167,51.15.70.112,51.254.100.139,51.255.129.15,
//      51.255.167.61,51.255.167.66,51.255.42.56,5.135.159.170,5.135.184.147,
//      5.135.186.30,5.175.192.200] any
//     (msg:"ET CNC Shadowserver Reported CnC Server IP group 29";
//     reference:url,doc.emergingthreats.net/bin/view/Main/BotCC;
//     reference:url,www.shadowserver.org; threshold: type limit, track by_src,
//     seconds 3600, count 1; flowbits:set,ET.Evil; flowbits:set,ET.BotccIP;
//     classtype:trojan-activity; sid:2404028; rev:4894;)
//
// Rather than copying all the raw data into our blocklist, we just include the
// SID which can be looked up at http://doc.emergingthreats.net/SID.
//
func parseBotnets(s *source, list blocklist, file *os.File) int {
	var cnt int

	ruleRE := regexp.MustCompile(`^alert ip.*\[((?:\d|\.|,)+)\].*sid:(\d+)`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		m := ruleRE.FindStringSubmatch(line)
		if len(m) == 3 {
			reason := "ET Botnet SID " + m[2]
			ips := strings.Split(m[1], ",")

			for _, ip := range ips {
				if addBlock(list, ip, reason) {
					cnt++
				}
			}

		}
	}

	return cnt
}

func parseWhitelist(s *source, list blocklist, file *os.File) int {
	cnt := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if addBlock(list, line, "") {
			cnt++
		}
	}
	return cnt
}

func fileSrc(name, meta string) (bool, error) {
	var oldHash string

	if file, err := ioutil.ReadFile(meta); err == nil {
		oldHash = string(file)
	}

	f, err := os.Open(name)
	if err != nil {
		err := fmt.Errorf("unable to read %s: %v", name, err)
		return false, err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		err := fmt.Errorf("unable to construct md5 of %s: %v", name, err)
		return false, err
	}

	newHash := fmt.Sprintf("%x", h.Sum(nil))
	if newHash != oldHash {
		ioutil.WriteFile(meta, []byte(newHash), 0644)
	}
	return oldHash != newHash, nil
}

// Download the latest upstream IP blacklists.  If anything has changed, rebuild
// our local combined blacklist.  The format is not quite a CSV file, in that it
// contains a comment and there are no headers, but many CSV readers can be told
// how to handle those things.  The one constraint is that the number of columns
// is consistent.
func buildBlocklist(stem string, sources []source) (bool, error) {
	tmpFile := stem + ".tmp"

	// Download the latest datasets
	updated := false
	for _, s := range sources {
		var refreshed bool
		var err error

		if s.url == "" {
			refreshed, err = fileSrc(s.file, s.file+".meta")
		} else {
			refreshed, err = common.FetchURL(s.url, s.file,
				s.file+".meta")
		}
		if err != nil {
			slog.Errorf("failed to refresh %s: %v", s.file, err)
		}
		updated = (updated || refreshed)
	}

	if !updated {
		return false, nil
	}

	// Parse the downloaded data sets and build a map containing the merged
	// data
	blocked := make(blocklist)
	for _, s := range sources {
		file, err := os.Open(s.file)
		if err != nil {
			slog.Errorf("unable to open %s: %v", s.file, err)
			continue
		}

		new := s.parser(&s, blocked, file)
		slog.Infof("Ingested %d addresses from %s\n", new, s.name)
		file.Close()
	}

	// Stored the merged dataset to disk
	f, err := os.Create(tmpFile)
	if err != nil {
		return false, fmt.Errorf("failed to create %s: %v", tmpFile, err)
	}
	fmt.Fprintf(f, "# built at %s\n", time.Now().Format(time.RFC3339))

	for ip, reason := range blocked {
		fmt.Fprintf(f, "%v,%s\n", ip, reason)
	}
	f.Close()
	os.Rename(tmpFile, stem+".csv")
	return true, nil
}

/***************************************************************************
 *
 * Google Cloud Storage support
 *
 ***************************************************************************/

// Make the provided object world-readable
func makePublic(ctx context.Context, obj *storage.ObjectHandle, name string) error {
	err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader)
	if err != nil {
		err = fmt.Errorf("failed to make %s public: %v", name, err)
	}
	return err
}

// Upload a local file to a google storage bucket, and make it world-readable
func uploadFile(ctx context.Context, bucket *storage.BucketHandle,
	local, remote string) error {

	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("unable to open local file %s: %v",
			local, err)
	}
	object := bucket.Object(remote)
	wc := object.NewWriter(ctx)
	_, err = io.Copy(wc, f)
	wc.Close()
	f.Close()

	if err != nil {
		err = fmt.Errorf("failed to upload %s to %s: %v", local,
			remote, err)
	} else {
		err = makePublic(ctx, object, remote)
	}

	return err
}

// Update the "latest" file in a google storage bucket
func updateLatest(ctx context.Context, bucket *storage.BucketHandle,
	stem, remote string) error {

	latest := stem + ".latest"
	object := bucket.Object(latest)
	wc := object.NewWriter(ctx)
	_, err := io.WriteString(wc, remote)
	wc.Close()

	if err != nil {
		err = fmt.Errorf("failed to update %s: %c", latest, err)
	} else {
		err = makePublic(ctx, object, latest)
	}
	return err
}

// Upload a blocklist to google cloud storage, make it publicly visible, and
// update the 'latest' pointer to it.
func uploadBlocklist(stem string) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to cloud storage: %v", err)
	}
	bucket := client.Bucket(gcpBucket)

	timestamp := time.Now().Format(time.RFC3339)
	remote := stem + "." + timestamp + ".csv"
	local := stem + ".csv"

	if err = uploadFile(ctx, bucket, local, remote); err != nil {
		return fmt.Errorf("failed to upload %s: %v", stem, err)
	}

	return updateLatest(ctx, bucket, stem, remote)
}

func main() {
	log, slog = daemonutils.SetupLogs()

	flag.Parse()
	if *helpFlag {
		fmt.Printf("usage: %s [-creds <gcp credentials file>]\n", os.Args[0])
		os.Exit(0)
	}

	if *credFile != "" {
		os.Setenv(gcpEnv, *credFile)
	}
	if f := os.Getenv(gcpEnv); f == "" {
		fmt.Printf("Must provide GCP credentials through -creds "+
			"option or %s envvar\n", gcpEnv)
		os.Exit(1)
	}

	if whitelist != nil {
		s := source{
			name:   "local DNS whitelist source",
			file:   *whitelist,
			parser: parseWhitelist,
		}
		whitelistSources = append(whitelistSources, s)
	}

	for _, dataset := range datasets {
		updated, err := buildBlocklist(dataset.stem, *dataset.sources)
		if !updated {
			if err != nil {
				slog.Errorf("Failed to build %s: %v\n", dataset.name, err)
			} else {
				slog.Infof("No changes upstream, not refreshing %s\n", dataset.name)
			}
		} else if err = uploadBlocklist(dataset.stem); err != nil {
			slog.Errorf("Failed to upload %s: %v\n", dataset.name, err)
		} else {
			slog.Infof("Uploaded %s to google cloud\n", dataset.name)
		}
	}
}
