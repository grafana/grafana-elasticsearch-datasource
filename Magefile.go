//go:build mage
// +build mage

package main

import (
	// mage:import
	build "github.com/grafana/grafana-plugin-sdk-go/build"
	"github.com/magefile/mage/mg"
)

// Default configures the default target.
var Default = BuildAllPlus

// BuildAllPlus builds all default targets plus linux/s390x and windows/arm64,
// to support bundling with Grafana builds on those platforms.
func BuildAllPlus() {
	b := build.Build{}
	mg.Deps(b.Linux, b.Windows, b.Darwin, b.DarwinARM64, b.LinuxARM64, b.LinuxARM, LinuxS390X, WindowsARM64)
}

// LinuxS390X builds the back-end plugin for Linux on s390x (IBM Z).
func LinuxS390X() error {
	return build.Build{}.Custom("linux", "s390x")
}

// WindowsARM64 builds the back-end plugin for Windows on arm64.
func WindowsARM64() error {
	return build.Build{}.Custom("windows", "arm64")
}
