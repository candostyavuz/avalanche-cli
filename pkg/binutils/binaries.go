// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package binutils

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/ux"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/coreos/go-semver/semver"
)

var (
	// interface compliance
	_ PluginBinaryDownloader = (*pluginBinaryDownloader)(nil)
	_ BinaryChecker          = (*binaryChecker)(nil)
)

type PluginBinaryDownloader interface {
	Download(vmID ids.ID, pluginDir, binDir string) error
}

type BinaryChecker interface {
	ExistsWithLatestVersion(name, binaryPrefix string) (bool, string, error)
}

type (
	binaryChecker          struct{}
	pluginBinaryDownloader struct {
		log logging.Logger
	}
)

func NewBinaryChecker() BinaryChecker {
	return &binaryChecker{}
}

func NewPluginBinaryDownloader(log logging.Logger) PluginBinaryDownloader {
	return &pluginBinaryDownloader{
		log: log,
	}
}

// InstallArchive installs the binary archive downloaded
func InstallArchive(ext string, archive []byte, binDir string) error {
	if ext == "zip" {
		return installZipArchive(archive, binDir)
	}
	return installTarGzArchive(archive, binDir)
}

// installZipArchive expects a byte stream of a zip file
func installZipArchive(zipfile []byte, binDir string) error {
	bytesReader := bytes.NewReader(zipfile)
	zipReader, err := zip.NewReader(bytesReader, int64(len(zipfile)))
	if err != nil {
		return fmt.Errorf("failed creating zip reader from binary stream: %w", err)
	}

	if err := os.MkdirAll(binDir, constants.DefaultPerms755); err != nil {
		return fmt.Errorf("failed to create app binary directory: %w", err)
	}

	// Closure to address file descriptors issue, uses Close to to not leave open descriptors
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed opening zip file: %w", err)
		}

		path := filepath.Join(binDir, f.Name)
		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(binDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, f.Mode()); err != nil {
				return fmt.Errorf("failed creating directory from zip entry: %w", err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(path), f.Mode()); err != nil {
				return fmt.Errorf("failed creating file from zip entry: %w", err)
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return fmt.Errorf("failed opening file from zip entry: %w", err)
			}

			_, err = io.Copy(f, rc)
			if err != nil {
				return fmt.Errorf("failed writing zip file entry to disk: %w", err)
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		if err := rc.Close(); err != nil {
			return err
		}
		return nil
	}

	for _, f := range zipReader.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

// installTarGzArchive expects a byte array in targz format
func installTarGzArchive(targz []byte, binDir string) error {
	byteReader := bytes.NewReader(targz)
	uncompressedStream, err := gzip.NewReader(byteReader)
	if err != nil {
		return fmt.Errorf("failed creating gzip reader from avalanchego binary stream: %w", err)
	}

	tarReader := tar.NewReader(uncompressedStream)
	for {
		header, err := tarReader.Next()
		switch {
		// if no more files are found return
		case err == io.EOF:
			return nil
		case err != nil:
			return fmt.Errorf("failed reading next tar entry: %w", err)
		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}
		// the target location where the dir/file should be created
		target := filepath.Join(binDir, header.Name)

		// check the file type
		switch header.Typeflag {
		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, constants.DefaultPerms755); err != nil {
					return fmt.Errorf("failed creating directory from tar entry %w", err)
				}
			}
		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed opening new file from tar entry %w", err)
			}
			// copy over contents
			if _, err := io.Copy(f, tarReader); err != nil {
				return fmt.Errorf("failed writing tar entry contents to disk: %w", err)
			}
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

// ExistsWithLatestVersion returns true if avalanchego can be found and at what path
// or false, if it can not be found (or an error if applies)
func (abc *binaryChecker) ExistsWithLatestVersion(binDir, binPrefix string) (bool, string, error) {
	// TODO this still has loads of potential pit falls
	// Should prob check for existing binary and plugin dir too
	match, err := filepath.Glob(filepath.Join(binDir, binPrefix) + "*")
	if err != nil {
		return false, "", err
	}
	var latest string
	switch len(match) {
	case 0:
		return false, "", nil
	case 1:
		latest = match[0]
	default:
		var semVers semver.Versions
		for _, v := range match {
			base := filepath.Base(v)
			newv, err := semver.NewVersion(base[len(binPrefix):])
			if err != nil {
				// ignore this one, it might be in an unexpected format
				// e.g. a dir which has nothing to do with this
				continue
			}
			semVers = append(semVers, newv)
		}

		sort.Sort(sort.Reverse(semVers))
		choose := fmt.Sprintf("v%s", semVers[0])
		for _, m := range match {
			if strings.Contains(m, choose) {
				latest = m
				break
			}
		}
	}
	return true, latest, nil
}

// getVMBinary downloads the binary from the binary server URL
func (d *pluginBinaryDownloader) Download(id ids.ID, pluginDir, binDir string) error {
	vmID := id.String()
	binaryPath := filepath.Join(pluginDir, vmID)
	info, err := os.Stat(binaryPath)
	if err == nil {
		if info.Mode().IsRegular() {
			d.log.Debug("binary already exists, skipping download")
			// remove all other plugins other than this one and `evm` for now.
			if err := cleanupPluginDir(vmID, pluginDir); err != nil {
				return err
			}
			return nil
		}
		return fmt.Errorf("binary plugin path %q was found but is not a regular file", binaryPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	binChecker := NewBinaryChecker()
	exists, latest, err := binChecker.ExistsWithLatestVersion(binDir, subnetEVMName+"-v")
	if err != nil {
		return fmt.Errorf("failed trying to locate plugin binary: %s", binDir)
	}
	if exists {
		d.log.Debug("local plugin binary found. skipping installation")
	} else {
		ux.Logger.PrintToUser("VM binary does not exist locally, starting download...")

		cancel := make(chan struct{})
		go ux.PrintWait(cancel)
		// TODO: we are hardcoding the release version at this point to 0.2.2
		// until we have a better binary, dependency and version management
		// as per https://github.com/ava-labs/avalanche-cli/pull/17#discussion_r887164924
		latestVer := "v0.2.2"
		/*
			latestVer, err := GetLatestReleaseVersion(constants.SubnetEVMReleaseURL)
			if err != nil {
				return fmt.Errorf("failed to get latest subnet-evm release version: %w", err)
			}
		*/

		latest, err = DownloadReleaseVersion(d.log, subnetEVMName, latestVer, binDir)
		if err != nil {
			return fmt.Errorf("failed downloading latest subnet-evm version: %w", err)
		}
		close(cancel)
		fmt.Println()
	}

	evmPath := filepath.Join(latest, subnetEVMName)

	if err := copyFile(evmPath, binaryPath); err != nil {
		return fmt.Errorf("failed copying latest subnet-evm to plugin dir: %w", err)
	}

	// remove all other plugins other than this one and `evm` for now.
	if err := cleanupPluginDir(vmID, pluginDir); err != nil {
		return err
	}

	return nil
}

// cleanupPluginDir removes all other plugins other than the given one and `evm` for now.
// TODO: this is only acceptable at this stage where ANR can't run multiple plugins anyways
// but should be later REMOVED if we support multiple plugins (with the caveat of a new
// tricky situation about to decide when and how to remove plugins from the plugin dir)
func cleanupPluginDir(vmID, pluginDir string) error {
	// list all plugins
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return err
	}

	pluginWhiteList := map[string]struct{}{
		"evm": {},
		vmID:  {},
	}

	for _, e := range entries {
		name := e.Name()
		if _, ok := pluginWhiteList[name]; !ok {
			if err := os.Remove(filepath.Join(pluginDir, name)); err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Chmod(constants.DefaultPerms755); err != nil {
		return err
	}
	return nil
}
