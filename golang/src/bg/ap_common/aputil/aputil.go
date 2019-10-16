/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/dhcp"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	nodeMode string
	nodeLock sync.Mutex
)

// Child is used to build and track the state of an child subprocess
type Child struct {
	Cmd     *exec.Cmd
	Process *os.Process
	env     []string

	pipes       int
	done        chan bool
	stdLogger   *log.Logger
	zapLogger   *zap.SugaredLogger
	zapLevel    zapcore.Level
	prefix      string
	softTimeout time.Duration
	logbuf      *circularBuf

	statName   string
	statusName string
	stat       *os.File
	status     *os.File

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
		line := c.prefix + scanner.Text()
		if c.stdLogger != nil {
			c.stdLogger.Printf("%s\n", line)
		}
		if c.zapLogger != nil {
			switch c.zapLevel {
			case zapcore.DebugLevel:
				c.zapLogger.Debugf("%s", line)
			case zapcore.InfoLevel:
				c.zapLogger.Infof("%s", line)
			case zapcore.WarnLevel:
				c.zapLogger.Warnf("%s", line)
			case zapcore.ErrorLevel:
				c.zapLogger.Errorf("%s", line)
			}
		}
		if c.logbuf != nil {
			c.logbuf.Write([]byte(line + "\n"))
		}
	}

	c.done <- true
}

// Start launches a prepared child process
func (c *Child) Start() error {
	if c.env != nil {
		c.Cmd.Env = c.env
	}
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
// finally with a more severe SIGKILL if the former didn't do the trick.  This
// abstraction exists to encapsulate this behavior and allow both processes as
// represented by the Child struct and process groups as represented by negative
// pids to be killed in this fashion.
func RetryKill(kill killFunc, alive aliveFunc, soft time.Duration) {
	sig := syscall.SIGTERM

	softDeadline := time.Now().Add(soft)
	for alive() {
		if time.Now().After(softDeadline) {
			sig = syscall.SIGKILL
		}
		// If the kill fails, it will continue to fail
		if err := kill(sig); err != nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// SetSoftTimeout allows the caller to override the default 100ms period before
// we switch from the 'soft' kill approach (SIGTERM) to the 'hard' approach
// (SIGKILL)
func (c *Child) SetSoftTimeout(soft time.Duration) {
	c.softTimeout = soft
}

// Stop stops a child process using the RetryKill() behavior.
func (c *Child) Stop() {
	if c == nil {
		return
	}

	kill := func(sig syscall.Signal) error { return c.Signal(sig) }
	alive := func() bool { return c.Process != nil }
	RetryKill(kill, alive, c.softTimeout)
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
			err := syscall.Kill(-pid, 0)
			return err != syscall.ESRCH
		}
		RetryKill(pgkill, pgalive, c.softTimeout)
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

	// Get the cpu usage stats from /proc/<pid>/stat
	if c.stat == nil {
		c.statName = fmt.Sprintf("/proc/%d/stat", c.Process.Pid)
		c.stat, err = os.Open(c.statName)
		if err != nil {
			return nil, err
		}
	}
	b := make([]byte, 2048)
	_, err = c.stat.ReadAt(b, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading %s: %v", c.statName, err)
	}
	tokens := strings.Split(string(b), " ")
	if len(tokens) < 52 {
		return nil,
			fmt.Errorf("unrecognized /proc/*/stat format")
	}
	rval.State = tokens[3]
	rval.Utime, _ = strconv.ParseUint(tokens[14], 10, 64)
	rval.Stime, _ = strconv.ParseUint(tokens[15], 10, 64)

	// Get the memory usage stats from /proc/<pid>/status
	if c.status == nil {
		c.statusName = fmt.Sprintf("/proc/%d/status", c.Process.Pid)
		c.status, err = os.Open(c.statusName)
		if err != nil {
			return nil, err
		}
	}

	fields := map[string]*uint64{
		"VmPeak:": &rval.VMPeak,
		"VmSize:": &rval.VMSize,
		"VmSwap:": &rval.VMSwap,
		"VmHWM:":  &rval.RssPeak,
		"VmRSS:":  &rval.RssSize,
	}

	found := 0
	_, err = c.status.ReadAt(b, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading %s: %v", c.statusName, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for (found < len(fields)) && scanner.Scan() {
		line := scanner.Text()
		if tokens := strings.Fields(line); len(tokens) >= 2 {
			if ptr, ok := fields[tokens[0]]; ok {
				kb, _ := strconv.ParseUint(tokens[1], 10, 64)
				*ptr = kb * 1024
				found++
			}
		}
	}

	return &rval, nil
}

// UseZapLog will shut down any open Go logging, and will prepare to log child
// output using zap.
func (c *Child) UseZapLog(prefix string, slog *zap.SugaredLogger,
	level zapcore.Level) {

	c.stdLogger = nil
	c.prefix = prefix
	childLogger, err := NewChildLogger()
	if err != nil {
		slog.Warnf("failed to init child logger: %v", err)
		c.zapLogger = slog
	} else {
		c.zapLogger = childLogger
	}
	if level < zapcore.DebugLevel || level > zapcore.ErrorLevel {
		slog.Warnf("invalid zap level.  Defaulting to InfoLevel\n")
		level = zapcore.InfoLevel
	}
	c.zapLevel = level
}

// UseStdLog will shut down any open zap logging, and will prepare to log child
// output using the standard Go log.
func (c *Child) UseStdLog(prefix string, flags int, w io.Writer) {
	if c.zapLogger != nil {
		c.zapLogger.Sync()
		c.zapLogger = nil
	}

	c.prefix = prefix
	if c.stdLogger == nil {
		c.stdLogger = log.New(w, "", flags)
	} else {
		c.stdLogger.SetOutput(w)
	}
}

// SetEnv prepares to set an environment variable in the child's environment.
// If the child is already running, the new setting will have no effect on the
// current instance - it will be applied after the next Stop()/Start().
func (c *Child) SetEnv(name, value string) {
	c.Lock()
	defer c.Unlock()
	if c.env == nil {
		c.env = os.Environ()
	}
	c.env = append(c.env, name+"="+value)
}

// LogPreserve tells us to keep a copy of the child's last sz bytes of log data
func (c *Child) LogPreserve(sz int) {
	c.logbuf = newCBuf(sz)
}

// LogContents returns the contents of the child's log buffer
func (c *Child) LogContents() string {
	var rval string

	if c.logbuf != nil {
		rval = string(c.logbuf.contents())
	}
	return rval
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
	c := &Child{
		pipes:       0,
		done:        make(chan bool),
		softTimeout: 100 * time.Millisecond,
	}

	c.Cmd = exec.Command(execpath, args...)
	if stdout, err := c.Cmd.StdoutPipe(); err == nil {
		c.pipes++
		go handlePipe(c, stdout)
	}
	if stderr, err := c.Cmd.StderrPipe(); err == nil {
		c.pipes++
		go handlePipe(c, stderr)
	}

	return c
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

// MacStrToProtobuf translates a mac address string into a uint64 suitable
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

// LegalModes is a map containing all of the legal per-node operating modes.
var LegalModes = map[string]bool{
	base_def.MODE_GATEWAY:   true,
	base_def.MODE_CORE:      true,
	base_def.MODE_SATELLITE: true,
	base_def.MODE_HTTP_DEV:  true,
}

// GetNodeMode returns the mode this node is running in.  If the return value is
// the empty string, it means that the DHCP server hasn't provided an
// authoritative signal as to the expected mode.
func GetNodeMode() string {
	var mode string

	nodeLock.Lock()
	defer nodeLock.Unlock()

	if nodeMode != "" {
		return nodeMode
	}

	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		lease, _ := dhcp.GetLease(iface.Name)
		if lease != nil && lease.Mode != "" {
			mode = lease.Mode
			break
		}
	}

	if mode != "" && !LegalModes[mode] {
		log.Fatalf("Illegal AP mode: %s\n", mode)
	}

	nodeMode = mode
	return nodeMode
}

// IsSatelliteMode checks to see whether this node is running as a mesh node
func IsSatelliteMode() bool {
	if os.Getenv("APMODE") == base_def.MODE_SATELLITE {
		return true
	}

	return GetNodeMode() == base_def.MODE_SATELLITE
}

// SortIntKeys takes an integer-indexed map and returns a sorted slice of the
// keys
func SortIntKeys(set interface{}) []int {
	slice := make([]int, 0)

	iter := reflect.ValueOf(set).MapRange()
	for iter.Next() {
		slice = append(slice, int(iter.Key().Int()))
	}
	sort.Ints(slice)
	return slice
}

// SortStringKeys takes a string-indexed map and returns a sorted slice of the
// keys
func SortStringKeys(set interface{}) []string {
	slice := make([]string, 0)

	iter := reflect.ValueOf(set).MapRange()
	for iter.Next() {
		slice = append(slice, iter.Key().String())
	}
	sort.Strings(slice)
	return slice
}
