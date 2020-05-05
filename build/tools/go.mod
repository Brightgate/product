module tools

go 1.12

require (
	github.com/golang/protobuf v1.3.2
	github.com/golangci/golangci-lint v1.17.1
	github.com/vektra/mockery v0.0.0-20181123154057-e78b021dcbb5
)

// The modules that depend on these are mistaken about the version/date/commit
// tuples, which are not consistent with what's in the repos, so we correct
// them.
replace (
	github.com/go-critic/go-critic v0.0.0-20181204210945-1df300866540 => github.com/go-critic/go-critic v0.3.5-0.20190526074819-1df300866540
	github.com/golangci/ineffassign v0.0.0-20180808204949-42439a7714cc => github.com/golangci/ineffassign v0.0.0-20190609212857-42439a7714cc
)
