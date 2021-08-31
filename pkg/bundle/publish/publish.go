package publish

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/RedHatGov/bundle/pkg/archive"
	"github.com/RedHatGov/bundle/pkg/config"
	"github.com/RedHatGov/bundle/pkg/config/v1alpha1"
	"github.com/RedHatGov/bundle/pkg/image"
	"github.com/RedHatGov/bundle/pkg/metadata/storage"
	"github.com/google/uuid"
	"github.com/mholt/archiver/v3"
	"github.com/openshift/oc/pkg/cli/image/mirror"
	"github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type UuidError struct {
	InUuid   uuid.UUID
	CurrUuid uuid.UUID
}

func (u *UuidError) Error() string {
	return fmt.Sprintf("Mismatched UUIDs. Want %v, got %v", u.CurrUuid, u.InUuid)
}

type SequenceError struct {
	inSeq   int
	CurrSeq int
}

func (s *SequenceError) Error() string {
	return fmt.Sprintf("Bundle Sequence out of order. Current sequence %v, incoming sequence %v", s.CurrSeq, s.inSeq)
}

func Publish(ctx context.Context, rootDir, archivePath, toMirror string, dryRun, skipTLS bool) error {

	logrus.Infof("Publish image set from archive %q to registry %q", archivePath, toMirror)

	tmpdir, err := ioutil.TempDir(rootDir, "imageset")
	if err != nil {
		return err
	}

	logrus.Debugf("Created temporary directory %s", tmpdir)
	defer os.RemoveAll(tmpdir)

	var currentMeta v1alpha1.Metadata
	var incomingMeta v1alpha1.Metadata

	a := archive.NewArchiver()

	filesinArchive, err := getImageSet(a, archivePath)

	if err != nil {
		return err
	}

	target := filepath.Join(config.PublishDir, config.MetadataFile)
	dest := filepath.Join(rootDir, target)

	// Create backend for rootDir
	backend, err := storage.NewLocalBackend(rootDir)
	if err != nil {
		return fmt.Errorf("error opening local backend: %v", err)
	}

	// Create a local workspace backend
	workspace, err := storage.NewLocalBackend(tmpdir)
	if err != nil {
		return fmt.Errorf("error opening local backend: %v", err)
	}

	// Copy metadata to publish dir

	if err != nil {
		return fmt.Errorf("error %s not found: %v", archivePath, err)
	}

	// Check for existing metadata
	if _, err := os.Stat(dest); os.IsNotExist(err) {

		logrus.Infof("No existing metadata found. Setting up new workspace")

		// Extract incoming metadata
		archive, ok := filesinArchive[config.MetadataFile]
		if !ok {
			return errors.New("metadata is not in archive")
		}

		logrus.Debug("Extracting incoming metadta ")
		if err := a.Extract(archive, target, tmpdir); err != nil {
			return err
		}

		// Find first file and load metadata from that
		if err := workspace.ReadMetadata(ctx, &incomingMeta, target); err != nil {
			return fmt.Errorf("error reading incoming metadata: %v", err)
		}

	} else {

		// Extract metadata incoming
		archive, ok := filesinArchive[config.MetadataFile]
		if !ok {
			return errors.New("metadata is not in archive")
		}

		logrus.Debug("Extract incoming metadata")
		if err := a.Extract(archive, target, tmpdir); err != nil {
			return err
		}

		// Compare metadata UID and sequence number
		if err := backend.ReadMetadata(ctx, &currentMeta, target); err != nil {
			return fmt.Errorf("error reading current metadata: %v", err)
		}

		if err := workspace.ReadMetadata(ctx, &incomingMeta, target); err != nil {
			return fmt.Errorf("error reading incoming metadata: %v", err)
		}

		logrus.Debug("Checking metadata UID")
		if incomingMeta.MetadataSpec.Uid != currentMeta.MetadataSpec.Uid {
			return &UuidError{currentMeta.MetadataSpec.Uid, incomingMeta.MetadataSpec.Uid}
		}

		logrus.Debug("Check metadata sequence number")
		currRun := currentMeta.PastMirrors[len(currentMeta.PastMirrors)-1]

		incomingRun := incomingMeta.PastMirrors[len(incomingMeta.PastMirrors)-1]

		if incomingRun.Sequence != (currRun.Sequence + 1) {
			return &SequenceError{incomingRun.Sequence, currRun.Sequence}
		}
	}

	// Load image associations to find layers not present locally.
	assocPath := filepath.Join(config.InternalDir, config.AssociationsFile)

	archive, ok := filesinArchive[config.AssociationsFile]
	if !ok {
		return errors.New("metadata is not in archive")
	}
	if err := a.Extract(archive, assocPath, tmpdir); err != nil {
		return err
	}

	assocs, err := readAssociations(filepath.Join(tmpdir, assocPath))
	if err != nil {
		return err
	}

	// For each image association with layers, pull any manifest layer digests needed
	// to reconstitute mirrored images and push those images.
	var (
		errs          []error
		manifestAssoc image.Association
		hasManifest   bool
	)
	for imageName, assoc := range assocs {

		assoc := assoc

		// Skip handling list-type images until all manifest layers have been pulled.
		if len(assoc.ManifestDigests) != 0 {
			// Validate that each manifest has an association as a sanity check.
			for _, manifestDigest := range assoc.ManifestDigests {
				if manifestAssoc, hasManifest = assocs[manifestDigest]; !hasManifest {
					errs = append(errs, fmt.Errorf("image %q: expected associations to have manifest %s but was not found", imageName, manifestDigest))
				}

				// Extract the manifest to destination path
				manifestPath := filepath.Join(assoc.Path, "manifests", manifestDigest)
				archive, ok := filesinArchive[manifestDigest]
				if ok {
					logrus.Debugf("Extracting manifest %s", manifestPath)
					if err := a.Extract(archive, manifestPath, tmpdir); err != nil {
						errs = append(errs, fmt.Errorf("image %q: cannot extract manifest %s from %s", imageName, manifestDigest, archive))
					}
					if _, err := os.Stat(filepath.Join(tmpdir, manifestPath)); err != nil {
						return err
					}
				}

				for _, layerDigest := range manifestAssoc.LayerDigests {
					logrus.Debugf("Processing layer %s for image %s", layerDigest, assoc.Name)
					// Construct blob path, which is adjacent to the manifests path.
					// If a layer exists in the archive (err == nil), extract it to the path
					blobPath := filepath.Join("blobs", layerDigest)
					archive, ok := filesinArchive[layerDigest]
					if ok {
						imagePath := filepath.Join(tmpdir, assoc.Path)
						if err := a.Extract(archive, blobPath, imagePath); err != nil {
							errs = append(errs, fmt.Errorf("access image %q blob %q at %s: %v", imageName, layerDigest, blobPath, err))
						}
						if _, err := os.Stat(filepath.Join(imagePath, blobPath)); err != nil {
							return err
						}
					} else {

						// Image layer must exist in the mirror registry since it wasn't archived,
						// so GET the layer and place it in the blob dir so it can be mirrored by `oc`.

						// TODO: implement layer pulling
					}
				}
			}

			if err := processImage(assoc, errs); err != nil {
				errs = append(errs, fmt.Errorf("error processing image %v: %v", manifestAssoc.Name, err))
			}
		}
	}

	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// import imagecontentsourcepolicy
	logrus.Info("ICSP importing not implemented")

	// import catalogsource
	logrus.Info("CatalogSource importing not implemented")

	// install imagecontentsourcepolicy
	logrus.Info("ICSP creation not implemented")

	// install catalogsource
	logrus.Info("CatalogSource creation not implemented")

	// Replace old metadata with new metadata

	if err := backend.WriteMetadata(context.Background(), &incomingMeta, target); err != nil {
		return err
	}

	return nil
}

func readAssociations(assocPath string) (assocs image.Associations, err error) {

	f, err := os.Open(assocPath)
	if err != nil {
		return assocs, fmt.Errorf("error opening image associations file: %v", err)
	}
	defer f.Close()

	return assocs, assocs.Decode(f)
}

func processImage(assoc image.Association, errs []error) error {

	// Build, checksum, and push manifest lists and index images.
	_ = assoc

	logrus.Debugf("Processing image %s", assoc.Name)

	// TODO: build manifest list or index depending on media type in the manifest file.

	// TODO: use image lib to checksum content (this might be done implicitly).

	// TODO: push to registry

	// TODO: delete the current image location

	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

// mirrortoReg will process a mirror mapping and push to the destination registry
func mirrorToReg(rootDir string, mappings []mirror.Mapping, dryRun, skipTLS bool) error {

	iostreams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	imageOpts := mirror.NewMirrorImageOptions(iostreams)
	imageOpts.FromFileDir = rootDir
	imageOpts.Mappings = mappings
	imageOpts.SecurityOptions.Insecure = skipTLS
	imageOpts.DryRun = dryRun

	return imageOpts.Run()
}

func getImageSet(a archive.Archiver, src string) (map[string]string, error) {

	filesinArchive := make(map[string]string)

	file, err := os.Stat(src)
	if err != nil {
		return nil, err
	}

	if file.IsDir() {

		// find first file and load metadata from that
		logrus.Infoln("Detected multiple incoming archive files")
		err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {

			if err != nil {
				return fmt.Errorf("traversing %s: %v", path, err)
			}
			if info == nil {
				return fmt.Errorf("no file info")
			}

			return a.Walk(path, func(f archiver.File) error {
				filesinArchive[f.Name()] = path
				return nil
			})
		})

	} else {
		err = a.Walk(src, func(f archiver.File) error {
			filesinArchive[f.Name()] = src
			return nil
		})
	}

	return filesinArchive, err
}
