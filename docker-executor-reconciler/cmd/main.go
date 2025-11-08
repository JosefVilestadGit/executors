package main

import (
	"github.com/colonyos/executors/docker-executor-reconciler/internal/cli"
	"github.com/colonyos/executors/docker-executor-reconciler/pkg/build"
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
