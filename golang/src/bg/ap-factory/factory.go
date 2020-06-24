//
// COPYRIGHT 2020 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
//

// ap-factory - factory-style install operations utility
//
// For MT7623-based systems, the various offsets and maximum sizes are derived
// from the corresponding "scatter file" given by MediaTek.  Scatter
// files are available in the "Hardware > MediaTek" team drive.
//
// References
//
// RFC3617, "Uniform Resource Identifier (URI) Scheme and Applicability
// Statement for the Trivial File Transfer Protocol (TFTP)".

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pin/tftp"
	"github.com/spf13/cobra"

	"bg/ap_common/platform"
	"bg/common/passwordgen"
)

const (
	noSide = iota
	sideA
	sideB
)

type slice struct {
	side    int
	device  string
	offset  int64
	maxSize int64
	src     string
}

type platformStorage struct {
	mainStorage  string
	sfdiskOutput map[string]string
	slices       map[string]slice
}

const (
	mt7623MainStorage = "/dev/mmcblk0"

	mt7623UBootOffset  = 0x40000
	mt7623UBootMaxSize = 0x80000

	// "A side" parameters.
	mt7623KernelOffset    = 0x140000
	mt7623KernelMaxSize   = 0x20000000
	mt7623RootfsDevice    = mt7623MainStorage + "p1"
	mt7623KernelOffsetBlk = "0xA00"

	// "B side" parameters.
	mt7623KernelXOffset    = 0x2140000
	mt7623RootfsXDevice    = mt7623MainStorage + "p2"
	mt7623KernelXOffsetBlk = "0x10A00"

	mt7623DataDevice = mt7623MainStorage + "p3"

	// Alternate overlay root paths.
	xRomDir     = "/tmp/x/rom"
	xOverlayDir = "/tmp/x/overlay"
	xRootDir    = "/tmp/x/root"
	xDataDir    = "/tmp/x/root/data"

	numPasswordCandidates = 5
)

var (
	mt7623emmcSfdisk8G = `label: dos
label-id: 0x00000000
device: /dev/mmcblk0
unit: sectors

/dev/mmcblk0p1 : start=      264192, size=     2097152, type=83
/dev/mmcblk0p2 : start=     2361345, size=     2097152, type=83
/dev/mmcblk0p3 : start=     4458497, size=    10811391, type=83
`
	mt7623emmcSfdisk4G = `label: dos
label-id: 0x00000000
device: /dev/mmcblk0
unit: sectors

/dev/mmcblk0p1 : start=      264192, size=     2097152, type=83
/dev/mmcblk0p2 : start=     2361345, size=     2097152, type=83
/dev/mmcblk0p3 : start=     4458497, size=     3274751, type=83
`
	mt7623slices = map[string]slice{
		"UBOOT":   {noSide, mt7623MainStorage, mt7623UBootOffset, mt7623UBootMaxSize, "UBOOT"},
		"KERNEL":  {sideA, mt7623MainStorage, mt7623KernelOffset, mt7623KernelMaxSize, "KERNEL"},
		"ROOTFS":  {sideA, mt7623RootfsDevice, 0x0, -1, "SQUASHFS"},
		"KERNELX": {sideB, mt7623MainStorage, mt7623KernelXOffset, mt7623KernelMaxSize, "KERNEL"},
		"ROOTFSX": {sideB, mt7623RootfsXDevice, 0x0, -1, "SQUASHFS"},
		"DATA":    {noSide, mt7623DataDevice, 0x0, -1, ""},
	}

	platforms = map[string]platformStorage{
		"mt7623": {mt7623MainStorage,
			map[string]string{
				"8GB": mt7623emmcSfdisk8G,
				"4GB": mt7623emmcSfdisk4G,
			},
			mt7623slices},
	}

	sides = []string{"no-side", "side-a", "side-b"}
)

var (
	dryRun           bool
	forceRepartition bool
	forceInstall     bool
	kernelOnly       bool
	imageDir         string
	installSide      string
	packages         []string
	targetPlatform   *platformStorage
	retrieveURL      string
	clearOverlay     bool
)

func getMainDevice() string {
	return targetPlatform.mainStorage
}

func getDataDevice() string {
	return targetPlatform.slices["DATA"].device
}

func getRootDevice(side int) string {
	for name, slice := range targetPlatform.slices {
		if slice.side == side && strings.Index(name, "ROOTFS") == 0 {
			return slice.device
		}
	}

	log.Fatalf("no such root device for %s", sides[side])

	return "/dev/null"
}

func partitionsAcceptable() bool {
	// Run sfdisk in a discovery mode.
	sfdisk := exec.Command("/usr/sbin/sfdisk", "-d", getMainDevice())
	result, err := sfdisk.Output()
	if err != nil {
		log.Fatalf("sfdisk dump failure: %s\n", err)
	}

	rs := string(result)
	for ak, ap := range targetPlatform.sfdiskOutput {
		if rs == ap {
			log.Printf("partition table for %s device found\n", ak)
			return true
		}
	}

	log.Printf("nonstandard partition table found %s\n", rs)

	return false
}

func repartitionSfdisk() {
	log.Printf("repartitioning %s\n", getMainDevice())
	// Run sfdisk in a modifying mode.
	sfdisk := exec.Command("/usr/sbin/sfdisk", getMainDevice())
	stdin, err := sfdisk.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}

	finishSfdisk := make(chan string)

	go func() {
		defer stdin.Close()
		// On the MT7623 platform, we have used both 4GB and 8GB
		// storage devices.  sfdisk(1) will correct an overlarge
		// final partition request to fit the actually available
		// device storage.  That behavior allows us to write the
		// 8GB partition map, and it will be correctly applied
		// to 4GB devices.
		io.WriteString(stdin, mt7623emmcSfdisk8G)
		finishSfdisk <- "written"
	}()

	<-finishSfdisk
	result, err := sfdisk.Output()
	if err != nil {
		log.Fatalf("sfdisk failure: %s\n", err)
	}

	log.Printf("sfdisk %s\n", result)

	time.Sleep(2 * time.Second)
	syscall.Sync()

	partprobe := exec.Command("partprobe", getMainDevice())
	result, err = partprobe.Output()
	if err != nil {
		log.Fatalf("partprobe failure: %s\n", err)
	}

	log.Printf("partprobe %s\n", result)
}

func writeSlice(sl slice, imd string) {
	devinfo, err := os.Stat(sl.device)
	if err != nil {
		log.Fatalf("stat %s from %s failed: %s\n", sl.device, sl.src, err)
	}

	dev, err := os.OpenFile(sl.device, os.O_WRONLY, devinfo.Mode())
	if err != nil {
		log.Fatalf("open %s failed: %s\n", sl.device, err)
	}
	defer dev.Close()

	off, err := dev.Seek(sl.offset, io.SeekStart)
	if err != nil {
		log.Fatalf("seek to %d on %s failed: %s\n", sl.offset, sl.device, err)
	}
	if off != sl.offset {
		log.Fatalf("seek to %d on %s arrived at %d\n", sl.offset, sl.device, off)
	}

	path := fmt.Sprintf("%s/%s", imd, sl.src)
	inf, err := os.OpenFile(path, os.O_RDONLY, 0x0)
	if err != nil {
		log.Fatalf("open %s failed: %s\n", path, err)
	}

	if dryRun {
		log.Printf("dry-run: skipping %s write\n", sl.src)
		return
	}

	wt, err := io.Copy(dev, inf)

	if err != nil {
		log.Printf("%s writeto failed: %s\n", sl.src, err)
	} else {
		log.Printf("%s wrote %d bytes\n", sl.src, wt)
	}

	if sl.maxSize > -1 && wt > sl.maxSize {
		log.Printf("WARNING: wrote %d bytes, exceeding %d maximum to %s\n", wt, sl.maxSize, sl.src)
	}
}

func writeSlices(imd string, side int) {
	for sn := range targetPlatform.slices {
		s := targetPlatform.slices[sn]

		if s.src != "" && (s.side == noSide || s.side == side) {
			if kernelOnly && s.src != "KERNEL" {
				log.Printf("kernel-only: skipping %s\n", s.src)
			} else {
				writeSlice(s, imd)
			}
		}
	}
}

func uBootEnvRead(vbl string) (string, error) {
	// This invocation can fail, if the environment variable is not
	// defined.
	printenv := exec.Command("/usr/sbin/fw_printenv", "-n", vbl)
	cb, err := printenv.Output()
	if err != nil {
		log.Printf("fw_printenv %s failed: %v\n", vbl, err)
		return "", err
	}

	return strings.TrimSpace(string(cb)), nil
}

func uBootEnvWrite(vbl string, value string, checkNeeded bool) {
	if checkNeeded {
		cval, err := uBootEnvRead(vbl)

		if err == nil && value == cval {
			return
		}
	}

	setenv := exec.Command("/usr/sbin/fw_setenv", vbl, value)
	_, err := setenv.Output()
	if err != nil {
		log.Fatalf("fw_setenv %s failed: %v\n", vbl, err)
	}

	log.Printf("fw_setenv updated %s to '%s'\n", vbl, value)
}

func writeUBootEnvironment(side int) {
	readoff := mt7623KernelOffsetBlk
	rootpart := mt7623RootfsDevice

	if side == sideB {
		readoff = mt7623KernelXOffsetBlk
		rootpart = mt7623RootfsXDevice
	}

	// Ensure valid menu items. Update serial programming menu items
	// to use YModem.
	uBootEnvWrite("boot0", "tftpboot; bootm", true)
	uBootEnvWrite("bootmenu_0",
		"1. System Load Linux to SDRAM via TFTP.=run boot0", true)
	uBootEnvWrite("boot1",
		"tftpboot;run boot_wr_img;run boot_rd_img;bootm", true)
	uBootEnvWrite("bootmenu_1",
		"2. System Load Linux Kernel then write to Flash via TFTP.=run boot1", true)
	uBootEnvWrite("boot2", "run boot_rd_img;bootm", true)
	uBootEnvWrite("bootmenu_2",
		"3. Boot system code via Flash.=run boot2", true)
	uBootEnvWrite("boot3",
		"tftpboot ${loadaddr} u-boot-mtk.bin;run wr_uboot", true)
	uBootEnvWrite("bootmenu_3",
		"4. System Load Boot Loader then write to Flash via TFTP.=run boot3", true)
	uBootEnvWrite("boot4",
		"loady;run boot_wr_img;run boot_rd_img;bootm", true)
	uBootEnvWrite("bootmenu_4",
		"5. System Load Linux Kernel then write to Flash via Serial.=run boot4", true)
	uBootEnvWrite("boot5", "loady;run wr_uboot", true)
	uBootEnvWrite("bootmenu_5",
		"6. System Load Boot Loader then write to Flash via Serial.=run boot5", true)

	// Ensure wr_uboot valid for unusual repair scenarios.
	uBootEnvWrite("wr_uboot",
		"uboot_check;if test ${uboot_result} = good; then mmc device 0;mmc write ${loadaddr} 0x200 0x200;reset; fi", true)

	// Update eMMC boot functions to use readoff for kernel data
	// location.
	uBootEnvWrite("boot_rd_img", "mmc device 0;mmc read ${loadaddr} ${readoff} 1;image_blks 512;mmc read ${loadaddr} ${readoff} ${img_blks}", true)

	uBootEnvWrite("boot_wr_img", "image_check; if test ${img_result} = good; then image_blks 512 ${filesize};mmc device 0;mmc write ${loadaddr} ${readoff} ${img_blks}; fi", true)

	// Confine relocations to first 256MB of kernel lowmem.
	uBootEnvWrite("bootm_size", "0x10000000", true)

	// Set default boot arguments and command.
	args := fmt.Sprintf("console=ttyS0,115200n8 root=%s earlyprintk", rootpart)
	uBootEnvWrite("bootargs", args, true)
	uBootEnvWrite("bootcmd", "run boot2", true)

	uBootEnvWrite("readoff", readoff, true)
}

func copyBusybox() {
	// Copy busybox to /tmp.  We typically want to do this prior to
	// rewriting the squashfs partition with the OS image, as that makes
	// the in-kernel inode state stale.
	cpbb := exec.Command("/bin/cp", "/bin/busybox", "/tmp")
	_, err := cpbb.Output()

	if err != nil {
		log.Printf("cp busybox failed: %v\n", err)
	}
}

func isFilesystemMounted(mtpt string) bool {
	mtab, err := os.OpenFile("/etc/mtab", os.O_RDONLY, 0)
	if err != nil {
		// If it's an I/O error, then we probably have had a disruptive
		// copy of the SQUASHFS image.
		log.Fatalf("could not open /etc/mtab: %v\n", err)
	}

	defer mtab.Close()

	scanner := bufio.NewScanner(mtab)
	for scanner.Scan() {
		mntln := scanner.Text()
		mntfld := strings.Split(mntln, " ")

		if mntfld[1] == mtpt {
			return true
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading /etc/mtab:", err)
	}

	return false
}

func isDirPresent(dpath string) bool {
	fi, err := os.Stat(dpath)
	if err != nil {
		return false
	}

	if fi.Mode().IsDir() {
		return true
	}

	return false
}

func createAbsentDir(dpath string) {
	if !isDirPresent(dpath) {
		log.Printf("creating '%s'", dpath)
		mustMkdirAll(dpath, 0755)
	}
}

// If you want to corrupt an instantiated F2FS filesystem, then
//
//     # dd if=/dev/random of=/dev/mmcblk0p3 bs=128K count=96
//
// should suffice.
func dataFilesystemAcceptable() bool {
	if !isFilesystemMounted("/data") {

		// Attempt a mount.
		err := syscall.Mount(getDataDevice(), "/data", "f2fs", syscall.MS_NOSUID, "")
		if err != nil {
			log.Printf("couldn't mount %s at /data: %v\n", getDataDevice(), err)
			return false
		}
	}

	createAbsentDir("/data/configd")
	createAbsentDir("/data/secret/rpcd")

	return true
}

func createDataFilesystem() {
	mkfs := exec.Command("/usr/sbin/mkfs.f2fs", "-f", getDataDevice())
	_, err := mkfs.Output()
	if err != nil {
		log.Printf("mkfs.f2fs failure: %s\n", err)
	}

	createAbsentDir("/data")

	if dataFilesystemAcceptable() {
		log.Printf("mounted newly created F2FS filesystem at /data\n")
	}
}

func retrieveFileHTTP(filename string) int64 {
	srcURL := fmt.Sprintf("%s/%s", retrieveURL, filename)
	hr, err := http.Get(srcURL)
	if err != nil {
		log.Fatalf("couldn't make http connection: %v\n", err)
	}
	defer hr.Body.Close()

	if hr.StatusCode == http.StatusOK {
		outfn := fmt.Sprintf("%s/%s", imageDir, filename)
		outf, err := os.Create(outfn)
		if err != nil {
			log.Fatalf("open '%s' failed: %v\n", outfn, err)
		}
		defer outf.Close()

		bw, err := io.Copy(outf, hr.Body)
		if err != nil {
			log.Fatalf("copy failed: %v\n", err)
		}

		return bw
	}

	log.Fatalf("GET %s operation unsuccessful: %d %v\n",
		srcURL, hr.StatusCode, http.StatusText(hr.StatusCode))

	return -1
}

func retrieveImagesHTTP() {
	for sn := range targetPlatform.slices {
		s := targetPlatform.slices[sn]

		if s.src == "" {
			continue
		}

		if s.side == noSide || s.side == sideA {
			bw := retrieveFileHTTP(s.src)
			log.Printf("%s wrote %d bytes\n", s.src, bw)
		}
	}

	for n, pn := range packages {
		bw := retrieveFileHTTP(pn)
		log.Printf("%d: %s wrote %d bytes\n", n, pn, bw)
	}
}

func retrieveFileTFTP(client *tftp.Client, filename string) int64 {
	wt, err := client.Receive(filename, "octet")
	if err != nil {
		log.Fatalf("tftp receive of '%s' failed: %s\n", filename, err)
	}

	outfn := fmt.Sprintf("%s/%s", imageDir, filename)
	outf, err := os.Create(outfn)
	if err != nil {
		log.Fatalf("open '%s' failed: %v\n", outfn, err)
	}
	defer outf.Close()

	bw, err := wt.WriteTo(outf)
	if err != nil {
		log.Fatalf("writeto failed: %v\n", err)
	}

	return bw
}

func retrieveImagesTFTP() {
	// Understand TFTP URLs, per RFC3617.
	srcURL, err := url.Parse(retrieveURL)
	port := "69"
	var srcHost string

	if srcURL.Port() != "" {
		srcHost = srcURL.Host
	} else {
		srcHost = fmt.Sprintf("%s:%s", srcURL.Host, port)
	}

	tc, err := tftp.NewClient(srcHost)
	if err != nil {
		log.Fatalf("couldn't make tftp client connection: %v\n", err)
	}

	tc.SetRetries(3)

	for sn := range targetPlatform.slices {
		s := targetPlatform.slices[sn]

		if s.src == "" {
			continue
		}

		if s.side == noSide || s.side == sideA {
			bw := retrieveFileTFTP(tc, s.src)
			log.Printf("%s wrote %d bytes\n", s.src, bw)
		}
	}

	for n, pn := range packages {
		bw := retrieveFileTFTP(tc, pn)
		log.Printf("%d: %s wrote %d bytes\n", n, pn, bw)
	}
}

func retrieve(cmd *cobra.Command, args []string) error {
	srcURL, err := url.Parse(retrieveURL)

	if err != nil {
		log.Fatalf("cannot parse URL '%s': %v\n", retrieveURL, err)
	}

	switch srcURL.Scheme {
	case "http":
		retrieveImagesHTTP()
	case "https":
		retrieveImagesHTTP()
	case "tftp":
		retrieveImagesTFTP()
	default:
		log.Fatalf("unrecognized URL scheme '%s': use 'tftp', 'http', or 'https'\n",
			srcURL.Scheme)
	}

	return nil
}

func chooseSide(pickSame bool) int {
	var blkdevID string
	var rootSide int
	var rootRamdisk bool
	var blkdev string

	mi, err := ioutil.ReadFile("/proc/self/mountinfo")
	if err != nil {
		log.Fatalf("cannot read /proc/self/mountinfo: %v\n", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(mi))

	for scanner.Scan() {
		l := scanner.Text()
		m := strings.Split(l, " ")

		if m[3] == "/" && m[4] == "/rom" && m[8] == "/dev/root" {
			blkdevID = m[2]
			rootRamdisk = false
		}

		// 0 0 0:1 / / rw - rootfs rootfs rw
		if m[3] == "/" && m[4] == "/" && m[8] == "rootfs" {
			blkdevID = m[2]
			rootRamdisk = true
		}
	}

	if blkdevID == "" {
		log.Fatalf("cannot find /dev/root in mountinfo\n")
	}

	readoff, err := uBootEnvRead("readoff")
	if err != nil {
		// When programming environment for the first time, the
		// readoff variable is not defined, and the various eMMC
		// boot variants are hard-coded to side A.
		readoff = mt7623KernelOffsetBlk
	}

	if rootRamdisk {
		switch readoff {
		case mt7623KernelOffsetBlk:
			if pickSame {
				return sideA
			}

			return sideB
		case mt7623KernelXOffsetBlk:
			if pickSame {
				return sideB
			}

			return sideA
		default:
			log.Fatalf("unrecognized 'readoff' value: %s\n", readoff)
		}
	}

	devlink := fmt.Sprintf("/sys/dev/block/%s", blkdevID)
	blkdev, err = os.Readlink(devlink)
	if err != nil {
		log.Fatalf("root device symlink read failure: %v\n", err)
	}

	blkdev = path.Base(blkdev)

	switch blkdev {
	case path.Base(mt7623RootfsDevice):
		rootSide = sideA
		log.Printf("rootfs device '%s' implies running %s", blkdev, sides[rootSide])
		if pickSame {
			return sideA
		}

		return sideB
	case path.Base(mt7623RootfsXDevice):
		rootSide = sideB
		log.Printf("rootfs device '%s' implies running %s", blkdev, sides[rootSide])
		if pickSame {
			return sideB
		}

		return sideA
	default:
		log.Fatalf("unknown rootfs device '%s'", blkdev)
	}

	return noSide
}

func checkMac() {
	macMediatekPrefix := regexp.MustCompile("^00:0[Cc]:[Ee]7")
	macBGAlphaPrefix := regexp.MustCompile("^60:90:84")

	// Check MAC address.
	mac, _ := uBootEnvRead("ethaddr")

	// XXX Add clause for proper MAC, once acquired.
	if macMediatekPrefix.MatchString(mac) {
		log.Printf("!! MAC unprogrammed (mediatek prefix)")
	} else if macBGAlphaPrefix.MatchString(mac) {
		log.Printf("MAC acceptable for alpha only")
	} else {
		log.Printf("!! MAC unknown: %s", mac)
	}
}

func overlayOpkgInstall(pkgname string) error {
	if !filepath.IsAbs(pkgname) {
		pkgname = filepath.Join(imageDir, pkgname)
	}
	opkg := exec.Command("/bin/opkg",
		"install", "-V2", "--offline-root", xRootDir,
		"--force-postinstall", pkgname)
	log.Printf("executing %v", opkg.Args)
	output, err := opkg.CombinedOutput()

	if err != nil {
		log.Printf("opkg install '%s' failed: %v", pkgname, err)
	}

	log.Printf("opkg install output:\n%s", output)
	return err
}

// This function reproduces the logic executed at the end of an OpenWrt
// build, where the rc.d links are calculated based on the START and
// STOP variable values in each init.d script.
func overlayParseInitDLinks(fpath string) {
	startRE := regexp.MustCompile(`^START=(\d+)`)
	stopRE := regexp.MustCompile(`^STOP=(\d+)`)

	idf, err := os.Open(fpath)
	if err != nil {
		log.Printf("unable to open %s, no symlinks made: %v", fpath, err)
		return
	}

	defer idf.Close()

	startSeen := false
	stopSeen := false

	scanner := bufio.NewScanner(idf)
	for scanner.Scan() {
		shln := scanner.Text()

		if !startSeen && startRE.MatchString(shln) {
			startSeen = true

			v := startRE.FindStringSubmatch(shln)
			if v == nil {
				log.Fatalf("inconsistency between MatchString() and FindStringSubmatch()")
			}

			oldf := fmt.Sprintf("../init.d/%s", path.Base(fpath))
			newf := fmt.Sprintf("%s/etc/rc.d/S%s%s", xRootDir, v[1], path.Base(fpath))

			link, err := os.Readlink(newf)
			if err != nil || link != oldf {
				os.Remove(newf)
				os.Symlink(oldf, newf)
				log.Printf("%s -> %s", oldf, newf)
			}
		} else if !stopSeen && stopRE.MatchString(shln) {
			stopSeen = true

			v := stopRE.FindStringSubmatch(shln)
			if v == nil {
				log.Fatalf("inconsistency between MatchString() and FindStringSubmatch()")
			}

			oldf := fmt.Sprintf("../init.d/%s", path.Base(fpath))
			newf := fmt.Sprintf("%s/etc/rc.d/K%s%s", xRootDir, v[1], path.Base(fpath))
			link, err := os.Readlink(newf)
			if err != nil || link != oldf {
				os.Remove(newf)
				os.Symlink(oldf, newf)
				log.Printf("%s -> %s", oldf, newf)
			}
		}

		// Once we have processed both variables, we are done.
		if startSeen && stopSeen {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("parsing '%s': %v", fpath, err)
	}
}

func overlayFixRcDLinks() {
	_ = filepath.Walk(fmt.Sprintf("%s/etc/init.d", xRootDir),
		func(fpath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.Mode().IsDir() {
				return nil
			}

			overlayParseInitDLinks(fpath)

			return nil
		})
}

func archiveCopy(pathGlob string) {
	var args []string

	dest := fmt.Sprintf("%s/%s", xRootDir, path.Dir(pathGlob))

	src, err := filepath.Glob(pathGlob)
	if len(src) < 1 {
		log.Printf("skipping empty glob %s", pathGlob)
		return
	}

	args = make([]string, 0)
	args = append(args, "-a")
	args = append(args, src...)
	args = append(args, dest)

	cp := exec.Command("/bin/cp", args...)
	_, err = cp.Output()

	if err != nil {
		log.Printf("cp '%s' -> '%s' failed: %v", pathGlob, dest, err)
		return
	}

	log.Printf("cp %+v -> %s completed", src, dest)
}

func mustMkdirAll(path string, mode os.FileMode) {
	err := os.MkdirAll(path, mode)
	if err != nil {
		log.Fatalf("couldn't MkdirAll '%s': %v", path, err)
	}
}

// struct squashfs_super_block {
// 	/*  0 */	uint32_t s_magic;
// 	/*  4 */	uint32_t inodes;
// 	/*  8 */	uint32_t mkfs_time;
// 	/* 12 */	uint32_t block_size;
// 	/* 16 */	uint32_t fragments;
// 	/* 20 */	uint16_t compression;
// 	/* 22 */	uint16_t block_log;
// 	/* 24 */	uint16_t flags;
// 	/* 26 */	uint16_t no_ids;
// 	/* 28 */	uint16_t s_major;
// 	/* 30 */	uint16_t s_minor;
// 	/* 32 */	uint64_t root_inode;
// 	/* 40 */	uint64_t bytes_used;
// 	/* 48 */	uint64_t id_table_start;
// 	/* 56 */	uint64_t xattr_id_table_start;
// 	/* 64 */	uint64_t inode_table_start;
// 	/* 72 */	uint64_t directory_table_start;
// 	/* 80 */	uint64_t fragment_table_start;
// 	/* 88 */	uint64_t lookup_table_start;
// } __attribute__((packed));

// Depressingly, this function reimplements the overlay-on-F2FS code
// path in OpenWrt's fstools.
func f2fsOverlay(side int, clearOverlay bool) {
	rootdev := getRootDevice(side)

	f, err := os.Open(rootdev)
	if err != nil {
		log.Fatalf("couldn't open: %+v", err)
	}

	var b []byte
	b = make([]byte, 96)

	n, err := f.Read(b)
	if err != nil {
		log.Fatalf("couldn't read: %+v", err)
	}

	if n < 96 {
		log.Fatal("must not be a squashfs superblock")
	}

	// 0..3 is s_magic
	// 36..44 is bytes_used
	magic := string(b[0:4])
	bytesUsed := binary.LittleEndian.Uint64(b[40:48])

	// How to figure out size of rootfs?
	// It's rounded to the nearest 64K.
	const rootdevOverlayAlign = 64 * 1024
	offset := (bytesUsed + (rootdevOverlayAlign - 1)) &^ (rootdevOverlayAlign - 1)

	log.Printf("magic: %s, bytesUsed: %d, offset: %d", magic, bytesUsed, offset)

	// Create loop device
	losetup := exec.Command("/usr/sbin/losetup",
		"-o", strconv.FormatUint(offset, 10),
		"-f", "--show", rootdev)
	result, err := losetup.Output()
	if err != nil {
		log.Fatalf("could not run /usr/sbin/losetup: %v\noutput %v\n", err, result)
	}

	loopback := strings.TrimSuffix(string(result), "\n")
	log.Printf("loopback %v", loopback)

	mustMkdirAll(xRomDir, 0755)
	mustMkdirAll(xOverlayDir, 0755)
	mustMkdirAll(xRootDir, 0755)

	syscall.Sync()

	// Mount rom.
	err = syscall.Mount(rootdev, xRomDir, "squashfs", 0, "")
	if err != nil {
		log.Fatalf("squashfs mount failed %v", err)
	}

	if !clearOverlay {
		err = syscall.Mount(loopback, xOverlayDir, "f2fs", 0, "")
		if err == nil {
			log.Printf("mounted f2fs at %s", xOverlayDir)
		} else {
			clearOverlay = true
		}
	}

	if clearOverlay {
		err = syscall.Unmount(xOverlayDir, 0)
		if err != nil {
			log.Printf("f2fs unmount failed %v", err)
		}

		log.Printf("clearing overlay")

		// Make F2FS filesystem and mount writeable.
		//
		// The mkfs.f2fs invocation may cause a kernel message like
		//
		//     print_req_error: I/O error, dev loop1, sector 0
		//
		// to be displayed (for the appropriate loop device).
		// This kernel message is triggered by an unsuccessful
		// SG_IO ioctl attempting to retrieve the geometry of
		// the eMMC device.
		//
		// Without the "rootfs_data" label, the volume_find()
		// functions in fstools:mount_root will fail.

		mkfs := exec.Command("/usr/sbin/mkfs.f2fs", "-f",
			"-l", "rootfs_data", loopback)
		result, err = mkfs.Output()

		if err != nil {
			log.Fatalf("mkfs failed: %+v %s", err, result)
		} else {
			log.Printf("mkfs output: %s", result)
		}

		err = syscall.Mount(loopback, xOverlayDir, "f2fs", 0, "")
		if err != nil {
			log.Fatalf("unable to mount %s f2fs at %s after mkfs: %v", loopback, xOverlayDir, err)
		}
	}

	upper := fmt.Sprintf("%s/upper", xOverlayDir)
	work := fmt.Sprintf("%s/work", xOverlayDir)

	mustMkdirAll(upper, 0755)
	mustMkdirAll(work, 0755)

	// Mount root (as overlay of rom and writeable).
	err = syscall.Mount("overlay", xRootDir, "overlay", syscall.MS_NOATIME,
		fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", xRomDir, upper, work))
	if err != nil {
		log.Fatalf("overlay mount failed %v", err)
	}

	// Prepare for opkg operations.
	varLock := fmt.Sprintf("%s/var/lock", xRootDir)
	mustMkdirAll(varLock, 0755)

	// /data mount must exist and be mounted.
	if !dataFilesystemAcceptable() {
		if dryRun {
			log.Println("dry-run: skipping /data creation")
		} else {
			createDataFilesystem()
		}
	}

	err = syscall.Mount("/data", xDataDir, "none", syscall.MS_BIND, "")
	if err != nil {
		log.Fatalf("bind overlay data mount failed %v", err)
	}

	// Mark overlay filesystem as ready for mounting by OpenWrt
	// fstools.  Failure to create this symbolic link will cause the
	// "overlay filesystem has not been fully initialized yet"
	// message to be displayed, and the overlay content to be
	// deleted.
	fsStateFile := fmt.Sprintf("%s/.fs_state", xOverlayDir)
	os.Remove(fsStateFile)
	err = os.Symlink("2", fsStateFile)
	if err != nil {
		log.Fatalf("symlink from '2' to %s/.fs_state failed: %v", xOverlayDir, err)
	}

	syscall.Sync()
}

func proposePasswords() {
	log.Printf("proposing %d candidate passwords", numPasswordCandidates)

	for n := 0; n < numPasswordCandidates; n++ {
		pw, err := passwordgen.HumanPassword(passwordgen.HumanPasswordSpec)
		if err != nil {
			log.Printf("proposePasswords couldn't generate password: %v", err)
			continue
		}
		log.Printf("possible password: %s", pw)
	}
}

func install(cmd *cobra.Command, args []string) error {
	side := noSide
	iS := strings.ToLower(installSide)
	switch iS {
	case "a":
		side = sideA
	case "b":
		side = sideB
	case "same":
		side = chooseSide(true)
	case "other":
		side = chooseSide(false)
	default:
		log.Fatalf("unrecognized install side '%s': use 'a', 'b', 'same', 'other'\n",
			installSide)
	}

	log.Printf("installing to side '%s'", sides[side])

	if len(packages) == 0 && !forceInstall {
		log.Fatalf("no packages provided in invocation; install aborted")
	}

	if dryRun {
		log.Println("dry-run: skipping busybox copy to /tmp")
	} else {
		copyBusybox()
	}

	// Are we partitioned correctly?
	if !partitionsAcceptable() || forceRepartition {
		if dryRun {
			// skip
			log.Println("dry-run: skipping repartitioning")
		} else {
			repartitionSfdisk()
		}
	}

	// Create /data, if needed.
	if !dataFilesystemAcceptable() {
		if dryRun {
			log.Println("dry-run: skipping /data creation")
		} else {
			createDataFilesystem()
		}
	}

	// Set U-Boot environment
	checkMac()
	if dryRun {
		log.Println("dry-run: skipping environment update")
	} else {
		writeUBootEnvironment(side)
	}

	// Copy images to appropriate on-device locations.
	writeSlices(imageDir, side)

	syscall.Sync()

	if dryRun {
		log.Println("dry-run: skipping overlay creation and installation")
	} else {
		// Prepare next root.
		f2fsOverlay(side, clearOverlay)

		// Propagate mutable files to next rootfs_data.  We may
		// manipulate these files in package postinstall scripts, so
		// propagation must take place prior to package operations.
		archiveCopy("/etc/passwd")
		archiveCopy("/etc/shadow")
		archiveCopy("/etc/group")
		archiveCopy("/etc/sudoers")
		archiveCopy("/etc/sudoers.d/*")
		archiveCopy("/etc/config/*")
		archiveCopy("/etc/ssh/*")

		syscall.Sync()

		// Install packages.
		for _, pn := range packages {
			if err := overlayOpkgInstall(pn); err != nil {
				return err
			}
			syscall.Sync()
		}

		// Post-packaging operations: fix rc.d symbolic links.
		overlayFixRcDLinks()

		// Put a symlink in the overlay that points to the release.json
		// ap.rpcd stashed on disk with the downloaded artifacts.  If
		// anything goes wrong, log the error and return, but don't make
		// ap-factory error out.
		defer syscall.Sync()
		absImageDir, err := filepath.Abs(imageDir)
		if err != nil {
			log.Printf("Can't get absolute path for %q: %v", imageDir, err)
			return nil
		}
		linkDir := platform.NewPlatform().ExpandDirPath(
			platform.APPackage, "etc")
		relPath, err := filepath.Rel(linkDir,
			filepath.Join(absImageDir, "release.json"))
		if err != nil {
			log.Printf("Release symlink failure: %v", err)
			return nil
		}
		curLinkPath := filepath.Join(xRootDir, linkDir, "release.json")
		// Since we install to a cleared overlay, this shouldn't exist,
		// but try anyway.
		err = os.Remove(curLinkPath)
		if perr, ok := err.(*os.PathError); ok {
			if serr, ok := perr.Err.(syscall.Errno); ok {
				if serr == syscall.ENOENT {
					err = nil
				}
			}
		}
		if err != nil {
			log.Printf("Failed to remove release symlink path: %v", err)
		}
		if err = os.Symlink(relPath, curLinkPath); err != nil {
			log.Printf("Failed to create release symlink: %v", err)
			return nil
		}
	}

	syscall.Sync()

	proposePasswords()

	return nil
}

func passwd(cmd *cobra.Command, args []string) error {
	proposePasswords()

	return nil
}

func mountOther(cmd *cobra.Command, args []string) error {
	side := chooseSide(false)

	if dryRun {
		log.Println("dry-run: skipping overlay creation and mount")
	} else {
		f2fsOverlay(side, false)
	}

	syscall.Sync()

	return nil
}

func umountOther(cmd *cobra.Command, args []string) error {
	var loopback string
	var err error

	// Unmount /data bind mount.
	if isFilesystemMounted(xDataDir) {
		err = syscall.Unmount(xDataDir, 0)
		if err != nil {
			log.Printf("overlay data unmount failed %v", err)
		}
	}

	// Unmount root (as overlay of rom and writeable)
	err = syscall.Unmount(xRootDir, 0)
	if err != nil {
		log.Printf("overlay unmount failed %v", err)
	}

	mounts, err := os.Open("/proc/self/mounts")
	if err != nil {
		log.Printf("unable to open /proc/self/mounts, loopback device will not be released: %v", err)
	} else {
		defer mounts.Close()

		// Deduce loopback from mountpoint backing device.
		scanner := bufio.NewScanner(mounts)
		for scanner.Scan() {
			mntln := scanner.Text()
			mntfld := strings.Split(mntln, " ")

			if mntfld[1] == xOverlayDir {
				loopback = mntfld[0]
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("reading /proc/self/mounts: %v", err)
		}
	}

	// Unmount writeable portion.
	err = syscall.Unmount(xOverlayDir, 0)
	if err != nil {
		log.Printf("f2fs unmount failed %v", err)
	}

	// Remove loopback.
	if loopback != "" {
		losetup := exec.Command("/usr/sbin/losetup", "-d", loopback)
		_, err := losetup.Output()
		if err != nil {
			log.Printf("losetup -d %s failed: %v", loopback, err)
		}
	}

	// Unmount ROM.
	err = syscall.Unmount(xRomDir, 0)
	if err != nil {
		log.Printf("squashfs unmount failed %v", err)
	}

	syscall.Sync()

	return nil
}

func harden(cmd *cobra.Command, args []string) error {
	// Require login for console access.
	ttylogin := exec.Command("/sbin/uci", "set", "system.@system[-1].ttylogin=1")
	_, err := ttylogin.Output()
	if err != nil {
		log.Fatalf("uci set failed: %v", err)
	}

	commit := exec.Command("/sbin/uci", "commit")
	_, err = commit.Output()
	if err != nil {
		log.Fatalf("uci commit failed: %v", err)
	}

	syscall.Sync()

	log.Printf("uci set to require console login")

	return nil
}

func flip(cmd *cobra.Command, args []string) error {
	side := noSide
	iS := strings.ToLower(installSide)
	switch iS {
	case "a":
		side = sideA
	case "b":
		side = sideB
	case "same":
		side = chooseSide(true)
	case "other":
		side = chooseSide(false)
	default:
		log.Fatalf("unrecognized side '%s': use 'a', 'b', 'same', 'other'\n",
			installSide)
	}
	log.Printf("pointing next boot to side %d", side)

	if dryRun {
		log.Println("dry-run: skipping environment update")
	} else {
		writeUBootEnvironment(side)
	}

	syscall.Sync()

	return nil
}

func status(cmd *cobra.Command, args []string) error {
	checkMac()

	kernelCmdline, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		log.Printf("Can't read /proc/cmdline: %v", err)
	} else {
		if strings.Contains(string(kernelCmdline), mt7623RootfsDevice) {
			log.Printf("kernel cmdline suggests currently running side A\n")
		} else if strings.Contains(string(kernelCmdline), mt7623RootfsXDevice) {
			log.Printf("kernel cmdline suggests currently running side B\n")
		} else {
			log.Fatalf("unknown root device in kernel cmdline\n")
		}
	}

	// Read readoff.
	roSide := noSide
	baSide := sideB
	readoff, err := uBootEnvRead("readoff")

	if err != nil {
		readoff = mt7623KernelOffsetBlk
	}

	switch readoff {
	case mt7623KernelOffsetBlk:
		log.Printf("read offset suggests side A on next boot\n")
		roSide = sideA
	case mt7623KernelXOffsetBlk:
		log.Printf("read offset suggests side B on next boot\n")
		roSide = sideB
	default:
		log.Fatalf("unrecognized 'readoff' value: %s\n", readoff)
	}

	// Read bootargs.
	bootargs, _ := uBootEnvRead("bootargs")

	if strings.Contains(bootargs, mt7623RootfsDevice) {
		log.Printf("root variable suggests side A on next boot\n")
		baSide = sideA
	} else if strings.Contains(bootargs, mt7623RootfsXDevice) {
		log.Printf("root variable suggests side B on next boot\n")
		baSide = sideB
	}

	if roSide == baSide {
		log.Printf("boot configuration consistent\n")
	} else {
		log.Fatalf("boot configuration inconsistent\n")
	}

	return nil
}

func verify(cmd *cobra.Command, args []string) error {
	// Existence of each of U-Boot, Kernel, Rootfs files.
	// Compare each file against on-device image.

	log.Fatalf("verify not implemented\n")

	return nil
}

func detectPlatform() *platformStorage {
	p := platforms[platform.NewPlatform().GetPlatform()]

	return &p
}

func main() {
	var err error

	packages = make([]string, 0)

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	targetPlatform = detectPlatform()

	rootCmd := &cobra.Command{
		Use: "ap-factory",
	}
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false,
		"dry-run, no modifications")
	rootCmd.PersistentFlags().StringVarP(&imageDir, "dir", "d", ".",
		"image download directory")

	retrieveCmd := &cobra.Command{
		Use:   "retrieve",
		Short: "Retrieve appliance software",
		Args:  cobra.NoArgs,
		RunE:  retrieve,
	}
	retrieveCmd.Flags().StringSliceVarP(&packages, "package", "P", nil,
		"additional packages to retrieve")
	retrieveCmd.Flags().StringVarP(&retrieveURL, "url", "u", "", "image source URL")

	rootCmd.AddCommand(retrieveCmd)

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install appliance software",
		Args:  cobra.NoArgs,
		RunE:  install,
	}
	installCmd.Flags().BoolVar(&forceInstall, "force-install", false,
		"proceed with install unconditionally")
	installCmd.Flags().BoolVar(&forceRepartition, "force-repartition", false,
		"always repartition storage device")
	installCmd.Flags().BoolVarP(&clearOverlay, "clear-overlay", "C", false,
		"force mkfs on overlay backing store")
	installCmd.Flags().BoolVarP(&kernelOnly, "kernel-only", "K", false,
		"kernel install only")
	installCmd.Flags().StringSliceVarP(&packages, "package", "P", nil,
		"additional, topologically-ordered packages to install")
	installCmd.Flags().StringVarP(&installSide, "side", "s", "other",
		"target install 'side' ['a', 'b', 'same', 'other']")
	rootCmd.AddCommand(installCmd)

	hardenCmd := &cobra.Command{
		Use:   "harden",
		Short: "Set hardened appliance configuration",
		Args:  cobra.NoArgs,
		RunE:  harden,
	}
	rootCmd.AddCommand(hardenCmd)

	flipCmd := &cobra.Command{
		Use:   "flip",
		Short: "Flip boot parameters to other (or named) side",
		Args:  cobra.NoArgs,
		RunE:  flip,
	}
	flipCmd.Flags().StringVarP(&installSide, "side", "s", "other",
		"target flip 'side' ['a', 'b', 'same', 'other']")
	rootCmd.AddCommand(flipCmd)

	passwdCmd := &cobra.Command{
		Use:   "passwd",
		Short: "Propose human readable password(s)",
		Args:  cobra.NoArgs,
		RunE:  passwd,
	}
	rootCmd.AddCommand(passwdCmd)

	mountOtherCmd := &cobra.Command{
		Use:     "mount-other",
		Aliases: []string{"mount-root"},
		Short:   "mount the other root partition and its overlay",
		Args:    cobra.NoArgs,
		RunE:    mountOther,
	}
	rootCmd.AddCommand(mountOtherCmd)

	umountOtherCmd := &cobra.Command{
		Use:     "umount-other",
		Aliases: []string{"umount-root"},
		Short:   "unmount the other root partition and its overlay",
		Args:    cobra.NoArgs,
		RunE:    umountOther,
	}
	rootCmd.AddCommand(umountOtherCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Report current appliance installation status",
		Args:  cobra.NoArgs,
		RunE:  status,
	}
	rootCmd.AddCommand(statusCmd)

	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify installed appliance software",
		Args:  cobra.NoArgs,
		RunE:  verify,
	}
	rootCmd.AddCommand(verifyCmd)

	if dryRun {
		log.Println("dry-run mode")
	}

	err = rootCmd.Execute()
	os.Exit(map[bool]int{true: 0, false: 1}[err == nil])
}
