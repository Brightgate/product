/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package aputil

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	dhcp "github.com/krolaw/dhcp4"
	"github.com/satori/uuid"
)

const (
	machineIDFile = "/etc/machine-id"
	dhcpDump      = "/sbin/dhcpcd"
)

var (
	nodeID   = uuid.Nil
	nodeMode string
	nodeLock sync.Mutex
)

// Child is used to build and track the state of an child subprocess
type Child struct {
	Cmd     *exec.Cmd
	Process *os.Process

	pipes  int
	done   chan bool
	logger *log.Logger
	prefix string
}

//
// Wait for stdout/stderr from a process, and print whatever it sends.  When the
// pipe is closed, notify our caller.
//
func handlePipe(c *Child, r io.ReadCloser) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if c.logger != nil {
			c.logger.Printf("%s%s\n", c.prefix, scanner.Text())
		} else {
			fmt.Printf("%s\n", scanner.Text())
		}
	}

	c.done <- true
}

// Start launches a prepared child process
func (c *Child) Start() error {
	err := c.Cmd.Start()
	if err == nil {
		c.Process = c.Cmd.Process
	}
	return err
}

// Wait waits for the child process to exit.  If we are capturing its output, we
// will wait for the stdin/stderr pipes to be closed.
func (c *Child) Wait() error {
	// Wait for the stdout/stderr pipes to close and for the child
	// process to exit
	for c.pipes > 0 {
		<-c.done
		c.pipes--
	}
	return c.Cmd.Wait()
}

// SetUID allows us to launch a child process with different credentials than
// the launching daemon.
func (c *Child) SetUID(uid, gid uint32) {
	cred := syscall.Credential{
		Uid: uid,
		Gid: gid,
	}

	attr := syscall.SysProcAttr{
		Credential: &cred,
	}

	c.Cmd.SysProcAttr = &attr
}

// SetOutput will reset a child's log target
func (c *Child) SetOutput(w io.Writer) {
	if c.logger == nil {
		return
	}

	c.logger.SetOutput(w)
}

// LogOutputTo will cause us to capture the stdin/stdout streams from a child
// process
func (c *Child) LogOutputTo(prefix string, flags int, w io.Writer) {
	c.logger = log.New(w, "", flags)
	c.prefix = prefix

	c.pipes = 0
	c.done = make(chan bool)
	if stdout, err := c.Cmd.StdoutPipe(); err == nil {
		c.pipes++
		go handlePipe(c, stdout)
	}
	if stderr, err := c.Cmd.StderrPipe(); err == nil {
		c.pipes++
		go handlePipe(c, stderr)
	}
}

// NewChild instantiates the tracking structure for a child process
func NewChild(execpath string, args ...string) *Child {
	var c Child

	c.Cmd = exec.Command(execpath, args...)

	return &c
}

// FileExists checks to see whether the file/directory at the path location
// exists
func FileExists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

// ProtobufToTime converts a Protobuf timestamp into the equivalent Go version
func ProtobufToTime(ptime *base_msg.Timestamp) *time.Time {
	if ptime == nil {
		return nil
	}

	sec := *ptime.Seconds
	nano := int64(*ptime.Nanos)
	tmp := time.Unix(sec, nano)
	return &tmp
}

// TimeToProtobuf converts a Go timestamp into the equivalent Protobuf version
func TimeToProtobuf(gtime *time.Time) *base_msg.Timestamp {
	if gtime == nil {
		return nil
	}

	tmp := base_msg.Timestamp{
		Seconds: proto.Int64(gtime.Unix()),
		Nanos:   proto.Int32(int32(gtime.Nanosecond())),
	}
	return &tmp
}

// NowToProtobuf gets the current time and returns a pointer to the Protobuf
// translation, which is suitable for embedding in a protobuf structure.
func NowToProtobuf() *base_msg.Timestamp {
	gtime := time.Now()
	return TimeToProtobuf(&gtime)
}

// ExpandDirPath takes a path name and will translate it into an
// APROOT-relative path if that incoming path starts with a single '/'.  If the
// path starts with anything else, it is returned unchanged.
func ExpandDirPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		// If the incoming path doesn't start with '/', then it's meant
		// to be relative from the current directory - not the root
		return path
	}
	if strings.HasPrefix(path, "//") {
		// If the incoming path starts with '//', then it's meant
		// to be an absolute path - not relative to APROOT
		return strings.TrimPrefix(path, "/")
	}

	root := os.Getenv("APROOT")
	if root == "" {
		root = "./"
	}
	return root + path
}

// GetNodeID reads /etc/machine-id, which contains a 128-bit, randomly
// generated ID that is unique to this device, converts it into the standard
// UUID format, and returns it to the caller.  On error, a NULL UUID is
// returned.
func GetNodeID() uuid.UUID {
	nodeLock.Lock()
	defer nodeLock.Unlock()

	if nodeID != uuid.Nil {
		// We've already read and parsed the machine-id file.  Return
		// the cached result
		return nodeID
	}

	file, err := ioutil.ReadFile(machineIDFile)
	if err != nil {
		log.Printf("Failed to read unique device ID from %s: %v\n",
			machineIDFile, err)
	} else if len(file) < 32 {
		log.Printf("Unique ID is only %d bytes long\n", len(file))
	} else {
		// The file contains 32 hex digits, which we need to
		// turn into a string that the UUID code can parse.
		s := string(file)
		uuidStr := fmt.Sprintf("%8s-%4s-%4s-%4s-%12s",
			s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
		nodeID, err = uuid.FromString(uuidStr)
		if err != nil {
			log.Printf("Failed to parse %s as a UUID: %v\n",
				uuidStr, err)
		}
	}

	return nodeID
}

// DHCPDecodeOptions parses a bytestream into a slice of DHCP options
func DHCPDecodeOptions(s []byte) (opts []dhcp.Option, err error) {
	end := len(s)
	idx := 0
	for idx+3 < end {
		code := s[idx]
		valLen := int(s[idx+1])
		idx += 2
		if valLen < 1 || idx+valLen > end {
			err = fmt.Errorf("illegal option length: %d", valLen)
			break
		}
		val := s[idx : idx+valLen]
		idx += valLen

		o := dhcp.Option{
			Code:  dhcp.OptionCode(code),
			Value: val,
		}
		opts = append(opts, o)
	}
	return
}

// DHCPEncodeOptions marshalls a slice of DHCP options into a bytestream as
// described in RFC-2132
func DHCPEncodeOptions(opts []dhcp.Option) (s []byte, err error) {
	for _, opt := range opts {
		if opt.Code == 0 || opt.Code >= dhcp.End {
			err = fmt.Errorf("bad option code: %d", opt.Code)
			break
		}

		s = append(s, byte(opt.Code))
		s = append(s, byte(len(opt.Value)))
		s = append(s, opt.Value...)
	}
	s = append(s, byte(dhcp.End))

	return
}

// Iterate over all of the wired network interfaces, looking for one that has
// a Brightgate-assigned address.  If we find one, look to see whether it has
// also assigned us an operating mode.
func getModeFromDHCP() string {
	var vendor, vendorOpt string

	// Extract the two components of a DHCP option from lines like:
	//   domain_name_servers='192.168.52.1'
	//   vendor_class_identifier='Brightgate, Inc.'
	//   vendor_encapsulated_options='0109736174656c6c697465ff'
	optionRE := regexp.MustCompile(`(\w+)='(.*)'`)

	all, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, i := range all {
		vendor = ""
		vendorOpt = ""

		if !strings.HasPrefix(i.Name, "eth") &&
			!strings.HasPrefix(i.Name, "enx") {
			continue
		}

		out, err := exec.Command(dhcpDump, "-4", "-U", i.Name).Output()
		if err != nil {
			continue
		}

		options := optionRE.FindAllStringSubmatch(string(out), -1)
		for _, opt := range options {
			if opt[1] == "vendor_class_identifier" {
				vendor = opt[2]
			}
			if opt[1] == "vendor_encapsulated_options" {
				vendorOpt = opt[2]
			}
		}

		if strings.Contains(vendor, "Brightgate") && vendorOpt != "" {
			break
		}
	}

	if vendorOpt != "" {
		// The vendor options are encapsulated in a binary stream of
		// [code, len, value] triples, which is then converted into a
		// binhex string.
		if s, err := hex.DecodeString(vendorOpt); err == nil {
			opts, _ := DHCPDecodeOptions(s)
			for _, o := range opts {
				if o.Code == 1 {
					return string(o.Value)
				}
			}
		}
	}

	return ""
}

var legalModes = map[string]bool{
	base_def.MODE_GATEWAY:   true,
	base_def.MODE_CORE:      true,
	base_def.MODE_SATELLITE: true,
}

// GetNodeMode returns the mode this node is running in
func GetNodeMode() string {
	var proposed string

	nodeLock.Lock()
	defer nodeLock.Unlock()

	if nodeMode != "" {
		return nodeMode
	}

	proposed = os.Getenv("APMODE")
	if proposed == "" {
		proposed = getModeFromDHCP()
	}

	if proposed == "" {
		proposed = base_def.MODE_GATEWAY
	}

	if !legalModes[proposed] {
		log.Fatalf("Illegal AP mode: %s\n", proposed)
	}
	nodeMode = proposed
	return nodeMode
}

// IsNodeMode checks to see whether this node is running in the given mode.
func IsNodeMode(check string) bool {
	mode := GetNodeMode()
	return (mode == check)
}

// IsSatelliteMode checks to see whether this node is running as a mesh node
func IsSatelliteMode() bool {
	return IsNodeMode(base_def.MODE_SATELLITE)
}
