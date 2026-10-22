package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// runningExecutableHash reads Linux's reference to the image actually running,
// even if its installed pathname has since been replaced. Executables may be
// root-owned: FileHash's stricter user-configuration ownership rules do not apply
// to this read-only identity check.
func runningExecutableHash() (string, error) {
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", fmt.Errorf("open running executable: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect running executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("running executable is not a regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("hash running executable: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
