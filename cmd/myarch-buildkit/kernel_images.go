package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
)

// Kernel images are system files. FileHash intentionally accepts only user-owned
// paths for configuration writes and restoration, so it must not be used here.
// Validate the root-owned path separately and hash the opened regular file.
func trustedKernelImageHash(path string) (string, error) {
	if err := safeRootPath("", path, false); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Size() == 0 || !ok || owner.Uid != 0 || info.Mode().Perm()&0022 != 0 {
		return "", fmt.Errorf("kernel image is not a nonempty, protected root-owned regular file: %s", path)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
