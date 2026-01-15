package native

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtarchie/ci/orchestra/cache"
)

// CopyToVolume implements cache.VolumeDataAccessor.
// Extracts a tar archive to the volume directory.
func (n *Native) CopyToVolume(_ context.Context, volumeName string, reader io.Reader) error {
	volumePath := filepath.Join(n.path, volumeName)

	// Ensure volume directory exists
	if err := os.MkdirAll(volumePath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Security: prevent path traversal
		target := filepath.Join(volumePath, header.Name)
		if !strings.HasPrefix(target, volumePath) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tr); err != nil {
				file.Close()

				return fmt.Errorf("failed to write file: %w", err)
			}

			file.Close()
		case tar.TypeSymlink:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	return nil
}

// CopyFromVolume implements cache.VolumeDataAccessor.
// Creates a tar archive of the volume directory contents.
func (n *Native) CopyFromVolume(_ context.Context, volumeName string) (io.ReadCloser, error) {
	volumePath := filepath.Join(n.path, volumeName)

	// Check if volume exists
	if _, err := os.Stat(volumePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("volume directory does not exist: %s", volumePath)
	}

	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)

		err := filepath.Walk(volumePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Get relative path within volume
			relPath, err := filepath.Rel(volumePath, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}

			// Skip the root directory itself
			if relPath == "." {
				return nil
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("failed to create tar header: %w", err)
			}

			header.Name = relPath

			// Handle symlinks
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(path)
				if err != nil {
					return fmt.Errorf("failed to read symlink: %w", err)
				}

				header.Linkname = linkTarget
			}

			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header: %w", err)
			}

			// Write file content for regular files
			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("failed to open file: %w", err)
				}

				defer file.Close()

				if _, err := io.Copy(tw, file); err != nil {
					return fmt.Errorf("failed to write file to tar: %w", err)
				}
			}

			return nil
		})

		if err != nil {
			tw.Close()
			pw.CloseWithError(err)

			return
		}

		if err := tw.Close(); err != nil {
			pw.CloseWithError(err)

			return
		}

		pw.Close()
	}()

	return pr, nil
}

var _ cache.VolumeDataAccessor = (*Native)(nil)
