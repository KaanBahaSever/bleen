//go:build darwin

package platform

import (
	"os"
	"path/filepath"
)

// Volumes lists mounted volumes under /Volumes, except the startup disk.
func Volumes() []Volume {
	des, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil
	}
	var out []Volume
	for _, de := range des {
		p := filepath.Join("/Volumes", de.Name())
		if target, err := os.Readlink(p); err == nil && target == "/" {
			continue // "Macintosh HD" is a link to the startup disk
		}
		if v, ok := volumeAt(p); ok {
			out = append(out, v)
		}
	}
	return out
}
