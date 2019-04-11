/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"bg/ap_common/aputil"
	"bg/ap_common/platform"
	"bg/base_def"
)

const (
	pname = "ap.mcp"

	nobodyUID = 65534 // uid for 'nobody'
	rootUID   = 0     // uid for 'root'
	pidfile   = "/var/tmp/ap.mcp.pid"
)

var (
	aproot   = flag.String("root", "", "Root of AP installation")
	apmode   = flag.String("mode", "", "Mode in which this AP should operate")
	cfgfile  = flag.String("c", "", "Alternate daemon config file")
	logname  = flag.String("l", "mcp.log", "where to send log messages")
	nodeFlag = flag.String("nodeid", "", "new value for device nodeID")
	platFlag = flag.String("platform", "", "hardware platform name")
	verbose  = flag.Bool("v", false, "more verbose logging")

	logfile *os.File

	plat *platform.Platform

	nodeName string
	nodeMode string
)

func reboot(from string) {
	const LinuxRebootCmdRestart = 0x1234567

	log.Printf("received reboot command from %s.  Rebooting now.", from)

	syscall.Sync()
	syscall.Reboot(LinuxRebootCmdRestart)
}

// The following logging routines are designed to allow this daemon's log output
// to match the formatting of the child daemons' Zap output.  We don't use Zap
// here because we are trying to interleave our own output with the child
// output, and don't want Zap to re-annotate the child output.
func logMsg(level, msg string) {
	file := "???"
	line := 0
	if _, path, l, ok := runtime.Caller(2); ok {
		pathFields := strings.Split(path, "/")
		if n := len(pathFields); n >= 2 {
			file = strings.Join(pathFields[n-2:], "/")
		} else {
			file = path
		}
		line = l
	}

	log.Printf("\t%s\t%s:%d\t%s\n", level, file, line, msg)
}

func logInfo(format string, v ...interface{}) {
	logMsg("INFO", fmt.Sprintf(format, v...))
}

func logWarn(format string, v ...interface{}) {
	logMsg("WARN", fmt.Sprintf(format, v...))
}

func logPanic(format string, v ...interface{}) {
	panic(fmt.Sprintf(format, v...))
}

func logDebug(format string, v ...interface{}) {
	if *verbose {
		logMsg("DEBUG", fmt.Sprintf(format, v...))
	}
}

func signalHandler() {
	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for done := false; !done; {
		s := <-sig
		switch s {

		case syscall.SIGHUP:
			reopenLogfile()
			logInfo("Reloading mcp.json")
			loadDefinitions()

		default:
			logInfo("Signal %v received, shutting down", s)
			done = true
		}
	}
}

func reopenLogfile() {
	if *logname == "" || *logname == "-" {
		return
	}

	path := plat.ExpandDirPath("__APDATA__", "mcp", *logname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logWarn("Unable to redirect logging to %s: %v", path, err)
		return
	}

	os.Stdout = f
	os.Stderr = f
	daemons.Lock()
	for _, d := range daemons.local {
		d.Lock()
		if d.child != nil {
			d.child.UseStdLog("", 0, f)
		}
		d.Unlock()
	}
	daemons.Unlock()

	if logfile == nil {
		os.Stdin, err = os.OpenFile("/dev/null", os.O_RDONLY, 0)
		if err != nil {
			logWarn("Couldn't close stdin")
		}
	} else {
		logInfo("Closing log")
		logfile.Close()
	}
	log.SetOutput(f)
	logInfo("Opened %s", path)
	logfile = f
}

func setEnvironment() {
	if *platFlag != "" {
		os.Setenv("APPLATFORM", *platFlag)
		platform.ClearPlatform()
	}

	plat = platform.NewPlatform()

	if err := verifyNodeID(); err != nil {
		logWarn("%v", err)
		os.Exit(1)
	}

	if *aproot == "" {
		binary, _ := os.Executable()
		logInfo("ap.mcp binary '%s'", binary)
		if strings.HasSuffix(binary, "opt/com.brightgate/bin/ap.mcp") {
			*aproot = strings.TrimSuffix(binary, "opt/com.brightgate/bin/ap.mcp")
		} else {
			wd, _ := os.Getwd()
			*aproot = wd
		}
		logInfo("aproot not set - using '%s'", *aproot)
	}
	os.Setenv("APROOT", *aproot)
	if nodeMode == base_def.MODE_SATELLITE {
		nodeName, _ = plat.GetNodeID()
	} else {
		nodeName = "gateway"
	}
}

func verifyNodeID() error {
	nodeID, err := plat.GetNodeID()

	if err == nil {
		var current, proposed string

		if *nodeFlag != "" {
			current = strings.ToLower(nodeID)
			proposed = strings.ToLower(*nodeFlag)
		}
		if current != proposed {
			logInfo("Not overriding existing nodeid: %s",
				current)
		}
		return nil
	}
	logWarn("Unable to get a device nodeID: %v", err)

	if *nodeFlag == "" {
		err = fmt.Errorf("must provide a device nodeID")

	} else if err = plat.SetNodeID(*nodeFlag); err != nil {
		err = fmt.Errorf("unable to set device nodeID: %v", err)
	} else {
		logInfo("Set new device nodeID: %s", *nodeFlag)
	}

	return err
}

// Check for the existence of /var/tmp/ap.mcp.pid.  If the file exists, check to
// see whether the pid it contains is still running as ap.mcp.  If it is,
// decline to start.  Otherwise, create the file with our PID.
func pidLock() error {
	var err error
	var data []byte

	if data, err = ioutil.ReadFile(pidfile); err == nil {
		pid := string(data)
		data, err = ioutil.ReadFile("/proc/" + pid + "/stat")
		if err == nil {
			fields := strings.Split(string(data), " ")
			if len(fields) > 2 && fields[1] == "(ap.mcp)" {
				return fmt.Errorf("another instance of mcp "+
					"appears to be running as pid %s", pid)
			}
		}
	}

	pid := strconv.Itoa(os.Getpid())
	err = ioutil.WriteFile(pidfile, []byte(pid), 0666)
	if err != nil {
		err = fmt.Errorf("unable to create %s: %v", pidfile, err)
	}
	return err
}

func profileInit() {
	go func() {
		err := http.ListenAndServe(base_def.MCP_DIAG_PORT, nil)
		logWarn("Profiler exited: %v", err)
	}()
}

// Shutdown all of the running daemons, and then exit
func shutdown(rval int) {
	all := "all"
	handleStop(selectTargets(&all))
	logInfo("MCP exiting")
	if logfile != nil {
		logfile.Close()
	}
	os.Remove(pidfile)
	os.Exit(rval)
}

func main() {
	var initMode string

	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime)

	reopenLogfile()
	if os.Geteuid() != rootUID {
		logWarn("mcp must be run as root")
		os.Exit(1)
	}

	if err := pidLock(); err != nil {
		log.Fatalf("%v", err)
	}
	defer os.Remove(pidfile)

	logInfo("ap.mcp (%d) coming online...", os.Getpid())

	if *apmode != "" {
		initMode = *apmode
	} else {
		initMode = aputil.GetNodeMode()
	}

	if aputil.LegalModes[initMode] {
		nodeMode = initMode
	} else if initMode == "" {
		nodeMode = base_def.MODE_GATEWAY
		logInfo("Can't determine mode.  Defaulting to %s.", nodeMode)
	} else {
		logPanic("Unrecognized node mode: %s", initMode)
	}

	setEnvironment()
	apiInit()
	profileInit()
	daemonInit()
	orphanCleanup()

	switch initMode {
	case base_def.MODE_SATELLITE:
		go satelliteLoop()
	case "":
		go modeMonitor()
	}

	logInfo("MCP online")
	signalHandler()
	shutdown(0)
}

func init() {
	plat = platform.NewPlatform()
}
