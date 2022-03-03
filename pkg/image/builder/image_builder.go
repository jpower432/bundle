package builder

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/sirupsen/logrus"
)

type ImageBuilder struct {
	NameOpts   []name.Option
	RemoteOpts []remote.Option
	Logger     *logrus.Entry
}

func (b *ImageBuilder) init() {
	if b.Logger == nil {
		b.Logger = logrus.NewEntry(logrus.New())
	}
}

type configUpdateFunc func(*v1.ConfigFile)

// Run modifies and pushes the catalog image existing in an OCI layout. The image configuration will be updated
// with the required labels and any provided layers will be appended.
func (b *ImageBuilder) Run(ctx context.Context, targetRef string, layoutPath layout.Path, update configUpdateFunc, layers ...v1.Layer) error {

	b.init()
	var v2format bool
	tag, err := name.NewTag(targetRef, b.NameOpts...)
	if err != nil {
		return err
	}

	idx, err := layoutPath.ImageIndex()
	if err != nil {
		return err
	}
	idxManifest, err := idx.IndexManifest()
	if err != nil {
		return err
	}

	for _, manifest := range idxManifest.Manifests {
		if manifest.MediaType == types.DockerManifestSchema2 {
			v2format = true
		}

		img, err := layoutPath.Image(manifest.Digest)
		if err != nil {
			return err
		}
		// Add new layers to image
		img, err = mutate.AppendLayers(img, layers...)
		if err != nil {
			return err
		}

		if update != nil {
			// Update image config
			cfg, err := img.ConfigFile()
			if err != nil {
				return err
			}
			update(cfg)
			img, err = mutate.Config(img, cfg.Config)
			if err != nil {
				return err
			}
		}

		layoutOpts := []layout.Option{}
		if manifest.Platform != nil {
			layoutOpts = append(layoutOpts, layout.WithPlatform(*manifest.Platform))
		}
		if err := layoutPath.ReplaceImage(img, match.Digests(manifest.Digest), layoutOpts...); err != nil {
			return err
		}
	}

	// Pull updated index
	idx, err = layoutPath.ImageIndex()
	if err != nil {
		return err
	}

	// Ensure the index media type is a docker manifest list
	// if child manifests are docker V2 schema
	if v2format {
		idx = mutate.IndexMediaType(idx, types.DockerManifestList)
	}
	return remote.WriteIndex(tag, idx, b.RemoteOpts...)
}

// CreateLayout will create an OCI image layout from an image or return
// a layout path from an existing OCI layout
func (b *ImageBuilder) CreateLayout(srcRef, dir string) (layout.Path, error) {
	b.init()
	if srcRef == "" {
		b.Logger.Debugf("Using existing OCI layout to %s", dir)
		return layout.FromPath(dir)
	}

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}
	// Pull source reference image
	ref, err := name.ParseReference(srcRef, b.NameOpts...)
	if err != nil {
		return "", err
	}
	idx, err := remote.Index(ref, b.RemoteOpts...)
	if err != nil {
		return "", err
	}
	b.Logger.Debugf("Writing OCI layout to %s", dir)
	return layout.Write(dir, idx)
}

// LayerFromFile will write the contents of the path(s) the target
// directory and build a v1.Layer
func LayerFromPath(targetPath, path string) (v1.Layer, error) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)

	pathInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	processPaths := func(hdr *tar.Header, info os.FileInfo) error {
		if !info.IsDir() {
			hdr.Size = info.Size()
		}

		if info.Mode().IsDir() {
			hdr.Typeflag = tar.TypeDir
		} else if info.Mode().IsRegular() {
			hdr.Typeflag = tar.TypeReg
		} else {
			return fmt.Errorf("not implemented archiving file type %s (%s)", info.Mode(), info.Name())
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}
		if !info.IsDir() {
			f, err := os.Open(filepath.Clean(path))
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				return fmt.Errorf("failed to read file into the tar: %w", err)
			}
			err = f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}

	if pathInfo.IsDir() {
		err := filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(path, fp)
			if err != nil {
				return fmt.Errorf("failed to calculate relative path: %w", err)
			}

			hdr := &tar.Header{
				Name: filepath.Join(targetPath, filepath.ToSlash(rel)),
				Mode: int64(info.Mode()),
			}
			if err := processPaths(hdr, info); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to scan files: %w", err)
		}

	} else {
		base := filepath.Base(path)
		hdr := &tar.Header{
			Name: filepath.Join(targetPath, filepath.ToSlash(base)),
			Mode: int64(pathInfo.Mode()),
		}
		if err := processPaths(hdr, pathInfo); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish tar: %w", err)
	}
	return tarball.LayerFromReader(&b)
}
