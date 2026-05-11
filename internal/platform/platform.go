package platform

import (
	"fmt"
	"runtime"
)

func Current() string {
	return fmt.Sprintf("%s-%s", osID(), archID())
}

func LibraryFilename(name string) string {
	switch runtime.GOOS {
	case "windows":
		return name + ".dll"
	case "darwin":
		return name + ".dylib"
	default:
		return name + ".so"
	}
}

func osID() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}

func archID() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}
