package main

import (
	"github.com/colonyos/executors/deployment-controller/internal/cli"
	"github.com/colonyos/executors/deployment-controller/pkg/build"
)

var (
	BuildVersion string = ""
	BuildTime    string = ""
)

func main() {
	build.BuildVersion = BuildVersion
	build.BuildTime = BuildTime
	cli.Execute()
}
