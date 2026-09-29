//go:build !windows

package source

import (
	"io"
	"io/fs"
	"os"
)

func openShared(p string) (io.ReadCloser, error) { return os.Open(p) }

func isSharingViolation(error) bool { return false }

func isCloudPlaceholder(fs.FileInfo) bool { return false }
