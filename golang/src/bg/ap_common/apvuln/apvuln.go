/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apvuln

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
)

// Three possible results of a single vulnerability test
const (
	Vulnerable = iota
	Cleared
	Error
)

// TestResult carries the output of a test through the aggregator into the scanner
type TestResult struct {
	State    int                    `json:"state"`
	Tool     string                 `json:"tool"`
	Name     string                 `json:"name"`
	Nickname string                 `json:"nickname"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

func defaultStr(found interface{}, failover string) string {
	var result string
	if found == nil {
		return failover
	}
	var ok bool
	if result, ok = found.(string); !ok {
		result = fmt.Sprintf("%v", found)
	}
	if len(result) > 0 {
		return result
	}
	return failover
}

// DetailsSummary returns a human-readable string describing the
// details of found vulnerabilities of a particular Tool
func (tr *TestResult) DetailsSummary() string {
	message := ""
	details := tr.Details

	// Currently in golang random order; ignoring index
	for _, detailInterface := range details {
		detail := detailInterface.(map[string]interface{})

		switch tr.Tool {
		case "nmap": // apvuln.NmapVulnerability
			message = fmt.Sprintf("%s%s", nmapDetails(detail), message)
		case "ap-inspect": // apvuln.InspectVulnerability
			message = fmt.Sprintf("%s%s", inspectDetails(detail), message)
		case "ap-defaultpass": // apvuln.DPvulnerability
			message = fmt.Sprintf("%s%s", dpDetails(detail), message)
		default:
			log.Printf("DetailsSummary unknown tool %s", tr.Tool)
		}
	}
	return message
}

func nmapDetails(detail map[string]interface{}) string {
	return fmt.Sprintf("Protocol: %s | Port: %s\n",
		defaultStr(detail["protocol"], "unknown"),
		defaultStr(detail["port"], "unknown"))
}

func inspectDetails(detail map[string]interface{}) string {
	return fmt.Sprintf("Program: %#v | Version: %s | "+
		"Service: %#v | Protocol: %s | Port: %s\n",
		defaultStr(detail["program"], "unknown"),
		defaultStr(detail["program_ver"], ""),
		defaultStr(detail["service"], "unknown"),
		defaultStr(detail["protocol"], "unknown"),
		defaultStr(detail["port"], "unknown"))
}

func dpDetails(detail map[string]interface{}) string {
	// Password: %#v\n
	// MUST END THE LINE SO IT CAN BE PARSED
	// (assumes no users or passwords contain \n)
	var creds = detail["credentials"].(map[string]interface{})
	return fmt.Sprintf("Service: %s | Protocol: %s | Port: %s | "+
		"User: %#v | Password: %#v\n",
		defaultStr(detail["service"], "unknown"),
		defaultStr(detail["protocol"], "unknown"),
		defaultStr(detail["port"], "unknown"),
		defaultStr(creds["username"], `""`),
		defaultStr(creds["password"], `""`))
}

// RepairedDPDetails takes a DP vulnerability and new credentials
// and renders a new string suitable for the details field.
//
// Returns an empty string on error
//
func RepairedDPDetails(dpVuln DPvulnerability, creds DPcredentials) string {
	return fmt.Sprintf("Service: %s | Protocol: %s | Port: %s | "+
		"New User: %#v | New Password: %#v | "+
		"Old User: %#v | Old Password: %#v\n",
		dpVuln.Service,
		dpVuln.Protocol,
		dpVuln.Port,
		creds.Username,
		creds.Password,
		dpVuln.Credentials.Username,
		dpVuln.Credentials.Password)
}

// ParseDetailsError is returned if ParseDetailsSummary has a problem
type ParseDetailsError struct {
	Message string
}

func (e ParseDetailsError) Error() string {
	return "ParseDetailsSummary: " + e.Message
}

// ParseDetailsSummary takes an object you want to populate
// and pulls its info out of ONE LINE of details.
//
// Client is responsible for splitting multiple details into
// multiple calls to ParseDetailsSummary
//
// Updates "object"; returns nil if all is well, or ParseDetailsError on error
func ParseDetailsSummary(object interface{}, details string) error {
	switch (object).(type) {
	case *NmapVulnerability:
		var ob *NmapVulnerability
		ob = object.(*NmapVulnerability)
		re := regexp.MustCompile(
			"nmapScript: (.*?) [|] Protocol: (.*?) [|] Port: (.*?)\n?")
		strs := re.FindStringSubmatch(details)
		if len(strs) < 2 {
			return ParseDetailsError{
				fmt.Sprintf("Didn't find NmapVulnerability: %s", details)}
		}
		log.Printf("%#v", strs)
		ob.Script = strs[1]
		ob.Protocol = strs[2]
		ob.Port = strs[3]
	case *InspectVulnerability:
		var ob *InspectVulnerability
		ob = object.(*InspectVulnerability)
		re := regexp.MustCompile(
			"Program: (.*?) [|] Version: (.*?) [|] Service: (.*?) " +
				"[|] Protocol: (.*?) [|] Port: (.*?)\n?$")
		strs := re.FindStringSubmatch(details)
		if len(strs) < 2 {
			return ParseDetailsError{
				fmt.Sprintf("Didn't find InspectVulnerability: %s", details)}
		}
		log.Printf("%#v", strs)
		ob.Program = strs[1]
		ob.ProgramVer = strs[2]
		ob.Service = strs[3]
		ob.Protocol = strs[4]
		ob.Port = strs[5]
	case *DPvulnerability:
		var ob *DPvulnerability
		ob = object.(*DPvulnerability)
		re := regexp.MustCompile(
			"Service: (.*?) [|] Protocol: (.*?) [|] Port: (.*?) " +
				"[|] User: (.*?) [|] Password: (.*?)\n?$")
		strs := re.FindStringSubmatch(details)
		if len(strs) < 2 {
			return ParseDetailsError{
				fmt.Sprintf("Didn't find DPvulnerability: %s", details)}
		}
		ob.Service = strs[1]
		ob.Protocol = strs[2]
		ob.Port = strs[3]
		var err error
		ob.Credentials.Username, err = strconv.Unquote(strs[4])
		if err != nil {
			return ParseDetailsError{
				fmt.Sprintf("bad username: %s", strs[4])}
		}
		ob.Credentials.Password, err = strconv.Unquote(strs[5])
		if err != nil {
			return ParseDetailsError{
				fmt.Sprintf("bad password: %s", "<redacted>")}
		}
	default:
		return ParseDetailsError{fmt.Sprintf("unknown type: %T", object)}
	}
	return nil
}

// Vulnerability data and arrays thereof differ in specifics
// according to the type of vulnerability and how it was found;
// this allows us to abstract away passing them around
//
// TODO: Harmonize with protobufs
//
type Vulnerability interface{}

// Vulnerabilities is typically an array of Vulnerability types
type Vulnerabilities []interface{}

// NmapVulnerability captures data we obtain from an nmap scan
//
type NmapVulnerability struct {
	IP       string `json:"ip"`
	IPType   string `json:"ipType,omitempty"`
	Port     string `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Service  string `json:"service,omitempty"`
	Script   string `json:"script"`
}

// InspectVulnProbe is used by ap-inspect to capture details about the vulnerabilities
//
// Finding any vulnerability sets Vulnerable to true.
// The associated Details for each discovered vulnerability
// are in the array of vulnerabilities.
//
type InspectVulnProbe struct {
	Vulnerable bool
	Vulns      Vulnerabilities
}

// InspectVulnerability represents one vulnerability discovered by ap-inspect
//
type InspectVulnerability struct {
	Identifier string `json:"identifier"` // e.g. "CVE-2018-6789"
	IP         string `json:"ip"`
	Protocol   string `json:"protocol"` // "tcp", "udp"
	Service    string `json:"service"`  // "smtp", "ssh", etc.
	Port       string `json:"port"`
	Program    string `json:"program"` // "exim", "dropbear", etc.
	ProgramVer string `json:"program_ver,omitempty"`
}

// DPcredentials are vulnerable credentials found by ap-defaultpass
//
type DPcredentials struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

// DPvulnerability represents one vulnerability discovered by ap-defaultpass
//
type DPvulnerability struct {
	IP          string        `json:"ip"`
	Protocol    string        `json:"protocol,omitempty"`
	Service     string        `json:"service"` // "smtp", "ssh", etc.
	Port        string        `json:"port"`
	Credentials DPcredentials `json:"credentials"`
}

// UnmarshalDPvulns unmarshals JSON representation of default password
// vulnerabilities (from ap-defaultpass)
//
func UnmarshalDPvulns(str []byte) (interface{}, error) {
	var target map[int]DPvulnerability
	return target, json.Unmarshal(str, &target)
}

// RDPVulnerability captures data we obtain from an rdpscan scan
//
type RDPVulnerability struct {
	IP      string `json:"ip"`
	Port    string `json:"port,omitempty"`
	Details string `json:"details,omitempty"`
}

// MarshalNotVulnerable creates a JSON object representing why a test
// is not vulnerable, including the reason.
//
// This accepts error, string or []byte types.
// It returns a JSON object {"0": <details>}
//
// If there are vulnerabilities, they are indexed
// beginning with "1". "0" is not a vulnerability.
//
func MarshalNotVulnerable(details interface{}) string {
	var reason = make(map[int]string)

	// vulnerabilities are 1-indexed; [0] means none
	if _, ok := details.(string); ok {
		reason[0] = details.(string)
	} else if _, ok := details.([]byte); ok {
		reason[0] = string(details.([]byte))
	} else if _, ok := details.(error); ok {
		reason[0] = fmt.Sprintf("%v", details)
	} else {
		log.Fatalf("MarshalNotVulnerable: bad arg type: %T\n", details)
	}
	result, err := json.Marshal(reason)
	if err != nil {
		log.Fatalf("MarshalNotVulnerable(%s) failed: %v\n", details, err)
	}
	return string(result)
}

// MarshalVulns marshals a set of vulnerabilities into a single JSON object
// representing an 1-indexed array of vulnerabilities.
// Index 0 represents "not vulnerable".
//
func MarshalVulns(vs []interface{}) ([]byte, error) {
	var indexed = make(map[int]interface{})
	for i, v := range vs {
		indexed[i+1] = v
	}
	return json.Marshal(indexed)
}
