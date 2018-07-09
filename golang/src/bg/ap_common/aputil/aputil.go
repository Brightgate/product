/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

const (
	dhcpDump = "/sbin/dhcpcd"
)

var (
	nodeMode string
	nodeLock sync.Mutex

	procStatusRE = regexp.MustCompile(`^(\w+):\s+(\d+)`)
)

// Profiler is a handle used by daemons to initiate profiling operations
type Profiler struct {
	name   string
	index  int
	active bool
}

// Child is used to build and track the state of an child subprocess
type Child struct {
	Cmd     *exec.Cmd
	Process *os.Process

	pipes  int
	done   chan bool
	logger *log.Logger
	prefix string

	stat   *os.File
	status *os.File

	setpgid bool

	sync.Mutex
}

// ChildState contains some basic statistics of a running child process
type ChildState struct {
	State   string
	VMSize  uint64 // Current vm size in bytes
	VMPeak  uint64 // Maximum vm size in bytes
	VMSwap  uint64 // Current swapped-out memory in bytes
	RssSize uint64 // Current in-core memory in bytes
	RssPeak uint64 // Maximum in-core memory in bytes
	Utime   uint64 // CPU consumed in user mode, in ticks
	Stime   uint64 // CPI consumed in kernel mode, in ticks
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
		c.Lock()
		c.Process = c.Cmd.Process
		c.Unlock()
	}
	return err
}

type killFunc func(syscall.Signal) error
type aliveFunc func() bool

// RetryKill stops a child process - first with a few gentle SIGTERMs, and
// finally with a more severe SIGKILL if the former didn't do the trick.  The
// call returns the signal used to terminate the process.  This abstraction
// exists to encapsulate this behavior and allow both processes as represented
// by the Child struct and process groups as represented by negative pids to be
// killed in this fashion.
func RetryKill(kill killFunc, alive aliveFunc) syscall.Signal {
	sig := syscall.SIGTERM
	attempts := 0
	sleeps := 0

	for alive() {
		if sleeps == 0 {
			if attempts > 5 {
				sig = syscall.SIGKILL
			}
			attempts++
			// If the kill fails, it will continue to fail
			if err := kill(sig); err != nil {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
		sleeps = (sleeps + 1) % 50
	}

	return sig
}

// Stop stops a child process using the RetryKill() behavior.
func (c *Child) Stop() syscall.Signal {
	if c == nil {
		return 0
	}

	kill := func(sig syscall.Signal) error { return c.Signal(sig) }
	alive := func() bool { return c.Process != nil }
	return RetryKill(kill, alive)
}

// Signal sends a signal to a child process
func (c *Child) Signal(sig os.Signal) error {
	var err error
	if c == nil {
		return err
	}

	c.Lock()
	if c.Process != nil {
		err = c.Process.Signal(sig)
	}
	c.Unlock()

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
	err := c.Cmd.Wait()

	c.Lock()
	if c.status != nil {
		c.status.Close()
		c.status = nil
	}
	if c.stat != nil {
		c.stat.Close()
		c.stat = nil
	}

	pid := c.Process.Pid
	c.Process = nil
	c.Unlock()

	// If we've set this child as a process group leader, then make sure we
	// kill its entire process group.
	if c.setpgid {
		pgkill := func(sig syscall.Signal) error {
			return syscall.Kill(-pid, sig)
		}
		pgalive := func() bool {
			err = syscall.Kill(-pid, 0)
			return err != syscall.ESRCH
		}
		RetryKill(pgkill, pgalive)
	}

	return err
}

// SetUID allows us to launch a child process with different credentials than
// the launching daemon.
func (c *Child) SetUID(uid, gid uint32) {
	cred := syscall.Credential{
		Uid: uid,
		Gid: gid,
	}

	// Be careful not to overwrite an existing SysProcAttr
	if c.Cmd.SysProcAttr == nil {
		c.Cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.Cmd.SysProcAttr.Credential = &cred
}

// GetPID returns the PID of the underlying process, or -1 if there is no
// process.
func (c *Child) GetPID() int {
	pid := -1

	if c != nil {
		c.Lock()
		if c.Process != nil {
			pid = c.Process.Pid
		}
		c.Unlock()
	}
	return pid
}

// GetState queries /proc for information about the child process and returns a
// ChildState structure.
func (c *Child) GetState() (*ChildState, error) {
	var rval ChildState
	var err error

	if c == nil {
		return nil, nil
	}

	c.Lock()
	defer c.Unlock()
	if c.Process == nil {
		return nil, nil
	}

	if c.stat == nil {
		path := fmt.Sprintf("/proc/%d/stat", c.Process.Pid)
		c.stat, err = os.Open(path)
		if err != nil {
			return nil, err
		}
	}
	b := make([]byte, 1024)
	_, err = c.stat.ReadAt(b, 0)
	if err == nil || err == io.EOF {
		tokens := strings.Split(string(b), " ")
		if len(tokens) < 52 {
			return nil,
				fmt.Errorf("unrecognized /proc/*/stat format")
		}
		rval.State = tokens[3]
		rval.Utime, _ = strconv.ParseUint(tokens[14], 10, 64)
		rval.Stime, _ = strconv.ParseUint(tokens[15], 10, 64)
	} else {
		log.Printf("Failed to read /proc/%d/stat: %v\n", c.Process.Pid, err)
	}

	if c.status == nil {
		path := fmt.Sprintf("/proc/%d/status", c.Process.Pid)
		c.status, err = os.Open(path)
		if err != nil {
			return nil, err
		}
	}

	fields := map[string]*uint64{
		"VmPeak": &rval.VMPeak,
		"VmSize": &rval.VMSize,
		"VmSwap": &rval.VMSwap,
		"VmHWM":  &rval.RssPeak,
		"VmRSS":  &rval.RssSize,
	}

	found := 0
	c.status.Seek(0, 0)
	scanner := bufio.NewScanner(c.status)
	for (found < len(fields)) && scanner.Scan() {
		line := scanner.Text()
		tokens := procStatusRE.FindStringSubmatch(line)
		if len(tokens) > 2 {
			field, val := tokens[1], tokens[2]
			if ptr := fields[field]; ptr != nil {
				kb, _ := strconv.ParseUint(val, 10, 64)
				*ptr = kb * 1024
				found++
			}
		}
	}

	return &rval, nil
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

// SetPgid with a true value will cause the child, when started, to be put into
// a new process group, as the process group leader.  When the child is reaped,
// its entire process group will be killed.
func (c *Child) SetPgid(setpgid bool) {
	c.setpgid = setpgid

	// Be careful not to overwrite an existing SysProcAttr
	if c.Cmd.SysProcAttr == nil {
		c.Cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.Cmd.SysProcAttr.Setpgid = setpgid
	c.Cmd.SysProcAttr.Pgid = 0
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

// IPStrToProtobuf translates an IP address string into a uint32 suitable
// for inserting into a protobuf.
func IPStrToProtobuf(ipstr string) *uint32 {
	var rval uint32

	if ip := net.ParseIP(ipstr).To4(); ip != nil {
		rval = binary.BigEndian.Uint32(ip)
	}
	return &rval
}

// MacStrToProtobuf translates a mac adress string into a uint64 suitable
// for inserting into a protobuf.
func MacStrToProtobuf(macstr string) *uint64 {
	var rval uint64

	if a, err := net.ParseMAC(macstr); err == nil {
		hwaddr := make([]byte, 8)
		hwaddr[0] = 0
		hwaddr[1] = 0
		copy(hwaddr[2:], a)
		rval = binary.BigEndian.Uint64(hwaddr)
	}
	return &rval
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

// LinuxBootTime retrieves the instance boot time using sysinfo(2)
func LinuxBootTime() time.Time {
	var z syscall.Sysinfo_t
	err := syscall.Sysinfo(&z)
	if err != nil {
		panic(err)
	}
	uptime := time.Duration(z.Uptime) * time.Second
	return time.Now().Add(-uptime)
}

var legalModes = map[string]bool{
	base_def.MODE_GATEWAY:   true,
	base_def.MODE_CORE:      true,
	base_def.MODE_SATELLITE: true,
	base_def.MODE_HTTP_DEV:  true,
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
		leases, _ := network.GetAllLeases()
		for _, lease := range leases {
			if proposed = lease.Mode; proposed != "" {
				break
			}
		}
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

// CPUStart begins collecting CPU profiling information
func (p *Profiler) CPUStart() error {
	p.index++
	name := fmt.Sprintf("%s.cpu.%03d.prof", p.name, p.index)
	file, err := os.Create(name)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", name, err)
	}
	log.Printf("Collecting cpu profile data in %s\n", name)
	pprof.StartCPUProfile(file)
	p.active = true
	return nil
}

// CPUStop stops collecting profiling information and writes out the results
func (p *Profiler) CPUStop() {
	if p.active {
		log.Printf("Stopping profiler\n")

		pprof.StopCPUProfile()
	}
	p.active = false
}

// HeapProfile writes the current heap profile to disk
func (p *Profiler) HeapProfile() {
	name := fmt.Sprintf("%s.mem.%03d.prof", p.name, p.index)
	file, err := os.Create(name)
	if err != nil {
		log.Printf("Failed to create heap profile: %v\n",
			err)
	} else {
		runtime.GC()
		pprof.WriteHeapProfile(file)
	}
}

// NewProfiler allocates an opaque handler daemons can use to perfom CPU and
// memory profiling operations.
func NewProfiler(name string) *Profiler {
	p := Profiler{
		name:   name,
		index:  0,
		active: false,
	}

	return &p
}
