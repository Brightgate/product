/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// The structure our "library" uses to track what packages are composed of what
// files and depend on what other packages.
type gopkg struct {
	name  string
	files []string
	deps  []string
}

// The structure used when unmarshaling the output of "go list -json"
type pkg struct {
	ImportPath string
	GoFiles    []string
	CgoFiles   []string
	Imports    []string
}

func getall(pkgs []string, library map[string]*gopkg) error {
	for outerDone := false; !outerDone; {
		args := []string{"list", "-json"}
		args = append(args, pkgs...)
		cmd := exec.Command("go", args...)
		output, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("Couldn't execute %s: %v", strings.Join(cmd.Args, " "), err)
		}
		if err = cmd.Start(); err != nil {
			return fmt.Errorf("Couldn't execute %s: %v", strings.Join(cmd.Args, " "), err)
		}
		decoder := json.NewDecoder(output)

		for {
			p := pkg{}
			if err = decoder.Decode(&p); err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			gp := &gopkg{name: p.ImportPath}
			gp.files = append(gp.files, p.GoFiles...)
			gp.files = append(gp.files, p.CgoFiles...)
			for _, imp := range p.Imports {
				if !strings.HasPrefix(imp, "bg/") || strings.HasPrefix(imp, "bg/vendor/") {
					continue
				}
				gp.deps = append(gp.deps, imp)
			}
			library[gp.name] = gp
		}

		if err = cmd.Wait(); err != nil {
			return fmt.Errorf("Command failed %s: %v", strings.Join(cmd.Args, " "), err)
		}

		// Run through the dependencies and make sure they're all
		// represented in the names; if not, run through again.
		pkgs = []string{}
		for _, pkg := range library {
			for _, dep := range pkg.deps {
				if _, ok := library[dep]; !ok {
					pkgs = append(pkgs, dep)
				}
			}
		}
		if len(pkgs) == 0 {
			outerDone = true
		}
	}

	return nil
}

func main() {
	library := make(map[string]*gopkg)

	err := getall(os.Args[1:], library)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	applib := make(map[string]*gopkg)
	cllib := make(map[string]*gopkg)
	deplib := make(map[string]*gopkg)

	for _, pkg := range library {
		if strings.HasPrefix(pkg.name, "bg/ap.") || strings.HasPrefix(pkg.name, "bg/ap-") {
			applib["$(APPBIN)/"+pkg.name[3:]] = pkg
		} else if strings.HasPrefix(pkg.name, "bg/cl.") || strings.HasPrefix(pkg.name, "bg/cl-") {
			cllib["$(CLOUDBIN)/"+pkg.name[3:]] = pkg
		} else {
			// This doesn't properly capture the "util" commands,
			// but we work around that in the Makefile.
			deplib["$(BGDEPDIR)/"+strings.Replace(pkg.name, "/", "--", -1)] = pkg
		}
	}

	f := func(targ string, pkg *gopkg) {
		sort.Strings(pkg.files)
		for i := range pkg.files {
			s := []string{}
			if pkg.name != "" {
				s = append(s, strings.Replace(pkg.name, "bg", "$(GOSRCBG)", 1))
			}
			s = append(s, pkg.files[i])
			pkg.files[i] = strings.Join(s, "/")
		}
		fmt.Printf("%s: \\\n\t%s\n", targ, strings.Join(pkg.files, " \\\n\t"))
		if len(pkg.deps) > 0 {
			depTargs := make([]string, len(pkg.deps))
			for i, dep := range pkg.deps {
				depTargs[i] = "$(BGDEPDIR)/" + strings.Replace(dep, "/", "--", -1)
			}
			sort.Strings(depTargs)
			fmt.Printf("%s: \\\n\t%s\n", targ, strings.Join(depTargs, " \\\n\t"))
		}
	}

	apptargs := make([]string, 0)
	for targ := range applib {
		apptargs = append(apptargs, targ)
	}
	sort.Strings(apptargs)
	for _, targ := range apptargs {
		f(targ, applib[targ])
	}
	fmt.Println()

	cltargs := make([]string, 0)
	for targ := range cllib {
		cltargs = append(cltargs, targ)
	}
	sort.Strings(cltargs)
	for _, targ := range cltargs {
		f(targ, cllib[targ])
	}
	fmt.Println()

	deptargs := make([]string, 0)
	for targ := range deplib {
		deptargs = append(deptargs, targ)
	}
	sort.Strings(deptargs)
	for _, targ := range deptargs {
		f(targ, deplib[targ])
	}
	fmt.Println()

	// Make godeps.mk depend on all the Go files, so that we can rebuild it
	// on every change, but not necessarily every invocation of make.
	maketargs := make([]string, 0)
	for _, pkg := range library {
		for i := range pkg.files {
			maketargs = append(maketargs, pkg.files[i])
		}
	}
	f("$(GOTOOLS_DIR)/godeps.mk", &gopkg{files: maketargs})
}
