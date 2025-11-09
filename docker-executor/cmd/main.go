package main

import (
	"github.com/colonyos/executors/docker/internal/cli"
	"github.com/colonyos/executors/docker/pkg/build"
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
