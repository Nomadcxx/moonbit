package main

import (
	"github.com/Nomadcxx/moonbit/internal/cli"
)

// Version and BuildTime are injected at link time:
//
//	go build -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
//
// The linker silently ignores -X for a symbol that does not exist, so these must
// stay declared here, in package main, with these exact names.
var (
	Version   = "dev"
	BuildTime = ""
)

func main() {
	cli.SetVersion(Version, BuildTime)
	cli.Execute()
}
