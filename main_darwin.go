//go:build darwin

package main

import (
	// Register the Apple Virtualization framework driver.
	// This is macOS-only due to cgo dependency on Apple's Virtualization.framework.
	_ "github.com/jtarchie/ci/orchestra/vz"
)
