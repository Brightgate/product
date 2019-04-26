//
// COPYRIGHT 2019 Brightgate Inc. All rights reserved.
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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"github.com/pin/tftp"
	"github.com/spf13/cobra"

	"bg/ap_common/platform"
)

type diskPart struct {
	number int
	start  int
	end    int
	ptype  int
}

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
	partitions   []diskPart
	sfdiskOutput string
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
)

var (
	mt7623emmcPartitions = []diskPart{
		{1, 264192, 2361343, 83},
		{2, 2361345, 4458496, 83},
		{3, 4458497, 152269887, 83},
	}

	mt7623emmcSfdisk = `label: dos
label-id: 0x00000000
device: /dev/mmcblk0
unit: sectors

/dev/mmcblk0p1 : start=      264192, size=     2097152, type=83
/dev/mmcblk0p2 : start=     2361345, size=     2097152, type=83
/dev/mmcblk0p3 : start=     4458497, size=    10811391, type=83
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
		"mediatek": {mt7623MainStorage, mt7623emmcPartitions,
			mt7623emmcSfdisk, mt7623slices},
	}
)

var (
	dryRun           bool
	forceRepartition bool
	kernelOnly       bool
	imageDir         string
	installSide      string
	targetPlatform   *platformStorage
	retrieveURL      string
)

func getMainDevice() string {
	return targetPlatform.mainStorage
}

func getDataDevice() string {
	return targetPlatform.slices["DATA"].device
}

func partitionsAcceptable() bool {
	// Run sfdisk in a discovery mode.
	sfdisk := exec.Command("/usr/sbin/sfdisk", "-d", getMainDevice())
	result, err := sfdisk.Output()
	if err != nil {
		log.Fatalf("sfdisk dump failure: %s\n", err)
	}

	rs := string(result)
	if rs == mt7623emmcSfdisk {
		return true
	}

	log.Printf("partition tables differ:\nfound %s\nexpected %s\n",
		rs, mt7623emmcSfdisk)
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

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, mt7623emmcSfdisk)
	}()

	result, err := sfdisk.Output()
	if err != nil {
		log.Fatalf("sfdisk failure: %s\n", err)
	}

	log.Printf("sfdisk %s\n", result)

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

	// Update serial programming menu items to use YModem.
	uBootEnvWrite("boot4", "loady;run boot_wr_img;run boot_rd_img;bootm", true)
	uBootEnvWrite("boot5", "loady;run wr_uboot", true)

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

// If you want to corrupt an instantiated F2FS filesystem, then
//
//     # dd if=/dev/random of=/dev/mmcblk0p3 bs=128K count=96
//
// should suffice.
func dataFilesystemAcceptable() bool {
	if isFilesystemMounted("/data") {
		return true
	}

	// Attempt a mount.
	err := syscall.Mount(getDataDevice(), "/data", "f2fs", syscall.MS_NOSUID, "")
	if err != nil {
		log.Printf("couldn't mount /data: %v\n", err)
		return false
	}

	if isFilesystemMounted("/data") {
		return true
	}

	return false
}

func createDataFilesystem() {
	mkfs := exec.Command("/usr/sbin/mkfs.f2fs", getDataDevice())
	_, err := mkfs.Output()
	if err != nil {
		log.Printf("mkfs.f2fs failure: %s\n", err)
	}

	if dataFilesystemAcceptable() {
		log.Printf("mounted newly created F2FS filesystem at /data\n")
	}
}

func retrieveImagesHTTP() {
	for sn := range targetPlatform.slices {
		s := targetPlatform.slices[sn]

		if s.src == "" {
			continue
		}

		if s.side == noSide || s.side == sideA {
			srcURL := fmt.Sprintf("%s/%s", retrieveURL, s.src)
			hr, err := http.Get(srcURL)
			if err != nil {
				log.Fatalf("couldn't make http connection: %v\n",
					err)
			}
			defer hr.Body.Close()

			if hr.StatusCode != http.StatusOK {
				log.Fatalf("HTTP operation unsuccessful: %d %v\n",
					hr.StatusCode,
					http.StatusText(hr.StatusCode))
			}

			outfn := fmt.Sprintf("%s/%s", imageDir, s.src)
			outf, err := os.Create(outfn)
			if err != nil {
				log.Fatalf("open '%s' failed: %v\n", outfn, err)
			}

			bw, err := io.Copy(outf, hr.Body)
			if err != nil {
				log.Fatalf("copy failed: %v\n", err)
			}
			log.Printf("%s wrote %d bytes\n", s.src, bw)
		}
	}
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

			wt, err := tc.Receive(s.src, "octet")
			if err != nil {
				log.Fatalf("tftp receive of '%s' failed: %s\n", s.src, err)
			}

			outfn := fmt.Sprintf("%s/%s", imageDir, s.src)
			outf, err := os.Create(outfn)
			if err != nil {
				log.Fatalf("open '%s' failed: %v\n", outfn, err)
			}

			bw, err := wt.WriteTo(outf)
			if err != nil {
				log.Fatalf("writeto failed: %v\n", err)
			}
			log.Printf("%s wrote %d bytes\n", s.src, bw)
		}
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
		log.Fatalf("unrecognized URL scheme '%s': use 'tftp'\n", srcURL.Scheme)
	}

	return nil
}

func chooseSide(pickSame bool) int {
	readoff, err := uBootEnvRead("readoff")
	if err != nil {
		// When programming environment for the first time, the
		// readoff variable is not defined, and the various eMMC
		// boot variants are hard-coded to side A.
		readoff = mt7623KernelOffsetBlk
	}

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
		log.Fatalf("unrecognized install side '%s': use 'a', 'b', 'same', 'other'\n", installSide)
	}

	var tS string
	switch side {
	case sideA:
		tS = "a"
	case sideB:
		tS = "b"
	}

	log.Printf("installing to side '%s'\n", tS)

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

	return nil
}

func status(cmd *cobra.Command, args []string) error {
	checkMac()

	// Read readoff.
	roSide := noSide
	baSide := sideB
	readoff, err := uBootEnvRead("readoff")

	if err != nil {
		readoff = mt7623KernelOffsetBlk
	}

	switch readoff {
	case mt7623KernelOffsetBlk:
		log.Printf("read offset suggests side A\n")
		roSide = sideA
	case mt7623KernelXOffsetBlk:
		log.Printf("read offset suggests side B\n")
		roSide = sideB
	default:
		log.Fatalf("unrecognized 'readoff' value: %s\n", readoff)
	}

	// Read bootargs.
	bootargs, _ := uBootEnvRead("bootargs")

	if strings.Contains(bootargs, mt7623RootfsDevice) {
		log.Printf("root variable suggests side A\n")
		baSide = sideA
	} else if strings.Contains(bootargs, mt7623RootfsXDevice) {
		log.Printf("root variable suggests side B\n")
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

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	targetPlatform = detectPlatform()

	rootCmd := &cobra.Command{
		Use: "ap-factory",
	}
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "dry-run, no modifications")
	rootCmd.PersistentFlags().StringVarP(&imageDir, "dir", "d", ".", "image download directory")

	retrieveCmd := &cobra.Command{
		Use:   "retrieve",
		Short: "Retrieve appliance software",
		Args:  cobra.NoArgs,
		RunE:  retrieve,
	}
	retrieveCmd.Flags().StringVarP(&retrieveURL, "url", "u", "", "image source URL")
	rootCmd.AddCommand(retrieveCmd)

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install appliance software",
		Args:  cobra.NoArgs,
		RunE:  install,
	}
	installCmd.Flags().BoolVarP(&forceRepartition, "force-repartition", "F", false, "always repartition storage device")
	installCmd.Flags().BoolVarP(&kernelOnly, "kernel-only", "K", false, "kernel install only")
	installCmd.Flags().StringVarP(&installSide, "side", "s", "other", "target install 'side' ['a', 'b', 'same', 'other']")
	rootCmd.AddCommand(installCmd)

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
