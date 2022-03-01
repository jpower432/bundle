package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/opencontainers/go-digest"
	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	imagemanifest "github.com/openshift/oc/pkg/cli/image/manifest"
	imgmirror "github.com/openshift/oc/pkg/cli/image/mirror"
	"github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/openshift/oc-mirror/pkg/archive"
	"github.com/openshift/oc-mirror/pkg/bundle"
	"github.com/openshift/oc-mirror/pkg/config"
	"github.com/openshift/oc-mirror/pkg/config/v1alpha2"
	"github.com/openshift/oc-mirror/pkg/image"
	"github.com/openshift/oc-mirror/pkg/metadata/storage"
)

type UuidError struct {
	InUuid   uuid.UUID
	CurrUuid uuid.UUID
}

func (u *UuidError) Error() string {
	return fmt.Sprintf("mismatched uuids, want %v, got %v", u.CurrUuid, u.InUuid)
}

type SequenceError struct {
	wantSeq int
	gotSeq  int
}

func (s *SequenceError) Error() string {
	return fmt.Sprintf("invalid bundle sequence order, want %v, got %v", s.wantSeq, s.gotSeq)
}

type ErrArchiveFileNotFound struct {
	filename string
}

func (e *ErrArchiveFileNotFound) Error() string {
	return fmt.Sprintf("file %s not found in archive", e.filename)
}

// Publish will plan a mirroring operation based on provided imageset on disk
func (o *MirrorOptions) Publish(ctx context.Context) (image.TypedImageMapping, error) {

	logrus.Infof("Publishing image set from archive %q to registry %q", o.From, o.ToMirror)

	var currentMeta v1alpha2.Metadata
	var incomingMeta v1alpha2.Metadata
	a := archive.NewArchiver()
	allMappings := image.TypedImageMapping{}
	var insecure bool
	if o.DestPlainHTTP || o.DestSkipTLS {
		insecure = true
	}

	// Set target dir for resulting artifacts
	if o.OutputDir == "" {
		dir, err := o.createResultsDir()
		o.OutputDir = dir
		if err != nil {
			return allMappings, err
		}
	}

	// Create workspace
	cleanup, tmpdir, err := mktempDir(o.Dir)
	if err != nil {
		return allMappings, err
	}

	// Handle cleanup of disk
	if !o.SkipCleanup {
		defer cleanup()
	}

	logrus.Debugf("Unarchiving metadata into %s", tmpdir)

	// Get file information from the source archives
	filesInArchive, err := bundle.ReadImageSet(a, o.From)
	if err != nil {
		return allMappings, err
	}

	// Extract imageset
	if err := o.unpackImageSet(a, tmpdir); err != nil {
		return allMappings, err
	}

	// Create a local workspace backend for incoming data
	workspace, err := storage.NewLocalBackend(tmpdir)
	if err != nil {
		return allMappings, fmt.Errorf("error opening local backend: %v", err)
	}
	// Load incoming metadta
	if err := workspace.ReadMetadata(ctx, &incomingMeta, config.MetadataBasePath); err != nil {
		return allMappings, fmt.Errorf("error reading incoming metadata: %v", err)
	}

	metaImage := o.newMetadataImage(incomingMeta.Uid.String())
	// Determine stateless or stateful mode
	var backend storage.Backend
	if incomingMeta.SingleUse {
		logrus.Warn("metadata has single-use label, using stateless mode")
		cfg := v1alpha2.StorageConfig{
			Local: &v1alpha2.LocalConfig{Path: o.Dir}}
		backend, err = storage.ByConfig(o.Dir, cfg)
		if err != nil {
			return allMappings, err
		}
		defer func() {
			if err := backend.Cleanup(ctx, config.MetadataBasePath); err != nil {
				logrus.Error(err)
			}
		}()
	} else {
		cfg := v1alpha2.StorageConfig{
			Registry: &v1alpha2.RegistryConfig{
				ImageURL: metaImage,
				SkipTLS:  insecure,
			},
		}
		backend, err = storage.ByConfig(o.Dir, cfg)
		if err != nil {
			return allMappings, err
		}
	}

	// Read in current metadata, if present
	switch err := backend.ReadMetadata(ctx, &currentMeta, config.MetadataBasePath); {
	case err != nil && !errors.Is(err, storage.ErrMetadataNotExist):
		return allMappings, err
	case err != nil:
		logrus.Infof("No existing metadata found. Setting up new workspace")
		// Check that this is the first imageset
		incomingRun := incomingMeta.PastMirror
		if incomingRun.Sequence != 1 {
			return allMappings, &SequenceError{1, incomingRun.Sequence}
		}
	default:
		// Complete metadata checks
		// UUID mismatch will now be seen as a new workspace.
		logrus.Debug("Check metadata sequence number")
		currRun := currentMeta.PastMirror
		incomingRun := incomingMeta.PastMirror
		if incomingRun.Sequence != (currRun.Sequence + 1) {
			return allMappings, &SequenceError{currRun.Sequence + 1, incomingRun.Sequence}
		}
	}

	// Unpack chart to user destination if it exists
	logrus.Debugf("Unpacking any provided Helm charts to %s", o.OutputDir)
	if err := unpack(config.HelmDir, o.OutputDir, filesInArchive); err != nil {
		return allMappings, err
	}

	// Load image associations to find layers not present locally.
	assocs, err := readAssociations(filepath.Join(tmpdir, config.AssociationsBasePath))
	if err != nil {
		return allMappings, err
	}

	toMirrorRef, err := imagesource.ParseReference(o.ToMirror)
	if err != nil {
		return allMappings, fmt.Errorf("error parsing mirror registry %q: %v", o.ToMirror, err)
	}
	logrus.Debugf("mirror reference: %#v", toMirrorRef)
	if toMirrorRef.Type != imagesource.DestinationRegistry {
		return allMappings, fmt.Errorf("destination %q must be a registry reference", o.ToMirror)
	}

	var errs []error

	for _, imageName := range assocs.Keys() {

		var mmapping []imgmirror.Mapping

		values, _ := assocs.Search(imageName)

		// Create temp workspace for image processing
		cleanUnpackDir, unpackDir, err := mktempDir(tmpdir)
		if err != nil {
			return allMappings, err
		}

		for _, assoc := range values {

			// Map of remote layer digest to the set of paths they should be fetched to.
			missingLayers := map[string][]string{}
			manifestPath := filepath.Join("v2", assoc.Path, "manifests")

			// Ensure child manifests are all unpacked
			logrus.Debugf("reading assoc: %s", assoc.Name)
			if len(assoc.ManifestDigests) != 0 {
				for _, manifestDigest := range assoc.ManifestDigests {
					if hasManifest := assocs.ContainsKey(imageName, manifestDigest); !hasManifest {
						errs = append(errs, fmt.Errorf("image %q: expected associations to have manifest %s but was not found", imageName, manifestDigest))
						continue
					}
					manifestArchivePath := filepath.Join(manifestPath, manifestDigest)
					switch _, err := os.Stat(manifestArchivePath); {
					case err == nil:
						logrus.Debugf("Manifest found %s found in %s", manifestDigest, assoc.Path)
					case errors.Is(err, os.ErrNotExist):
						if err := unpack(manifestArchivePath, unpackDir, filesInArchive); err != nil {
							errs = append(errs, err)
						}
					default:
						errs = append(errs, fmt.Errorf("accessing image %q manifest %q: %v", imageName, manifestDigest, err))
					}
				}
			}

			// Unpack association main manifest
			if err := unpack(filepath.Join(manifestPath, assoc.ID), unpackDir, filesInArchive); err != nil {
				errs = append(errs, err)
				continue
			}

			for _, layerDigest := range assoc.LayerDigests {
				logrus.Debugf("Found layer %v for image %s", layerDigest, imageName)
				// Construct blob path, which is adjacent to the manifests path.
				blobPath := filepath.Join("blobs", layerDigest)
				imagePath := filepath.Join(unpackDir, "v2", assoc.Path)
				imageBlobPath := filepath.Join(imagePath, blobPath)
				aerr := &ErrArchiveFileNotFound{}
				switch err := unpack(blobPath, imagePath, filesInArchive); {
				case err == nil:
					logrus.Debugf("Blob %s found in %s", layerDigest, assoc.Path)
				case errors.Is(err, os.ErrNotExist) || errors.As(err, &aerr):
					// Image layer must exist in the mirror registry since it wasn't archived,
					// so fetch the layer and place it in the blob dir so it can be mirrored by `oc`.
					missingLayers[layerDigest] = append(missingLayers[layerDigest], imageBlobPath)
				default:
					errs = append(errs, fmt.Errorf("accessing image %q blob %q at %s: %v", imageName, layerDigest, blobPath, err))
				}
			}

			m := imgmirror.Mapping{Name: assoc.Name}
			if m.Source, err = imagesource.ParseReference("file://" + assoc.Path); err != nil {
				errs = append(errs, fmt.Errorf("error parsing source ref %q: %v", assoc.Path, err))
				continue
			}

			if assoc.TagSymlink != "" {
				if err := unpack(filepath.Join(manifestPath, assoc.TagSymlink), unpackDir, filesInArchive); err != nil {
					errs = append(errs, err)
					continue
				}
				m.Source.Ref.Tag = assoc.TagSymlink
			}

			m.Source.Ref.ID = assoc.ID
			m.Destination = toMirrorRef
			m.Destination.Ref.Name = m.Source.Ref.Name
			m.Destination.Ref.Tag = m.Source.Ref.Tag
			m.Destination.Ref.ID = m.Source.Ref.ID
			m.Destination.Ref.Namespace = path.Join(o.UserNamespace, m.Source.Ref.Namespace)

			// Add references for the mirror mapping
			mmapping = append(mmapping, m)

			// Add top level assocation to the ICSP mapping
			if assoc.Name == imageName {
				source, err := imagesource.ParseReference(imageName)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				allMappings.Add(source, m.Destination, assoc.Type)
			}

			if len(missingLayers) != 0 {
				// Fetch all layers and mount them at the specified paths.
				if err := o.fetchBlobs(ctx, currentMeta, missingLayers); err != nil {
					return allMappings, err
				}
			}
		}

		// Mirror all mappings for this image
		if len(mmapping) != 0 {
			if err := o.publishImage(mmapping, unpackDir); err != nil {
				errs = append(errs, err)
			}
		}

		// Cleanup temp image processing workspace as images are processed
		if !o.SkipCleanup {
			cleanUnpackDir()
		}
	}
	if len(errs) != 0 {
		return allMappings, utilerrors.NewAggregate(errs)
	}

	logrus.Debug("rebuilding catalog images")

	found, err := o.unpackCatalog(tmpdir, filesInArchive)
	if err != nil {
		return allMappings, err
	}

	if found {
		ctlgRefs, err := o.rebuildCatalogs(ctx, tmpdir)
		if err != nil {
			return allMappings, fmt.Errorf("error rebuilding catalog images from file-based catalogs: %v", err)
		}
		if err := WriteCatalogSource(ctlgRefs, o.OutputDir); err != nil {
			return allMappings, err
		}
		allMappings.Merge(ctlgRefs)
	}

	// Replace old metadata with new metadata
	if err := backend.WriteMetadata(ctx, &incomingMeta, config.MetadataBasePath); err != nil {
		return allMappings, err
	}

	return allMappings, nil
}

// readAssociations will process and return data from the image associations file
func readAssociations(assocPath string) (assocs image.AssociationSet, err error) {
	f, err := os.Open(filepath.Clean(assocPath))
	if err != nil {
		return assocs, fmt.Errorf("error opening image associations file: %v", err)
	}
	defer f.Close()

	return assocs, assocs.Decode(f)
}

// unpackImageSet unarchives all provided tar archives	if err != nil {
func (o *MirrorOptions) unpackImageSet(a archive.Archiver, dest string) error {

	// archive that we do not want to unpack
	exclude := []string{"blobs", "v2", config.HelmDir}

	file, err := os.Stat(o.From)
	if err != nil {
		return err
	}

	if file.IsDir() {

		err = filepath.Walk(o.From, func(path string, info os.FileInfo, err error) error {

			if err != nil {
				return fmt.Errorf("traversing %s: %v", path, err)
			}
			if info == nil {
				return fmt.Errorf("no file info")
			}

			extension := filepath.Ext(path)
			extension = strings.TrimPrefix(extension, ".")

			if extension == a.String() {
				logrus.Debugf("Extracting archive %s", path)
				if err := archive.Unarchive(a, path, dest, exclude); err != nil {
					return err
				}
			}

			return nil
		})

	} else {

		logrus.Infof("Extracting archive %s", o.From)
		if err := archive.Unarchive(a, o.From, dest, exclude); err != nil {
			return err
		}
	}

	return err
}

// TODO(estroz): symlink blobs instead of copying them to avoid data duplication.
// `oc` mirror libs should be able to follow these symlinks.
func copyBlobFile(src io.Reader, dstPath string) error {
	logrus.Debugf("copying blob to %s", dstPath)
	if err := os.MkdirAll(filepath.Dir(dstPath), os.ModePerm); err != nil {
		return err
	}
	// Allowing exisitng files to be written to for now since we
	// some blobs appears to be written multiple time
	// TODO: investigate this issue
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return fmt.Errorf("error creating blob file: %v", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("error copying blob %q: %v", filepath.Base(dstPath), err)
	}
	return nil
}

func (o *MirrorOptions) fetchBlobs(ctx context.Context, meta v1alpha2.Metadata, missingLayers map[string][]string) error {
	var insecure bool
	if o.DestPlainHTTP || o.DestSkipTLS {
		insecure = true
	}
	restctx, err := image.CreateDefaultContext(insecure)
	if err != nil {
		return err
	}

	var errs []error
	for layerDigest, dstBlobPaths := range missingLayers {
		imgRef, err := o.findBlobRepo(meta.PastBlobs, layerDigest)
		if err != nil {
			errs = append(errs, fmt.Errorf("error finding remote layer %q: %v", layerDigest, err))
		}
		if err := o.fetchBlob(ctx, restctx, imgRef.Ref, layerDigest, dstBlobPaths); err != nil {
			errs = append(errs, fmt.Errorf("layer %s: %v", layerDigest, err))
			continue
		}
	}

	return utilerrors.NewAggregate(errs)
}

// fetchBlob fetches a blob at <o.ToMirror>/<resource>/blobs/<layerDigest>
// then copies it to each path in dstPaths.
func (o *MirrorOptions) fetchBlob(ctx context.Context, restctx *registryclient.Context, ref reference.DockerImageReference, layerDigest string, dstPaths []string) error {
	var insecure bool
	if o.DestPlainHTTP || o.DestSkipTLS {
		insecure = true
	}
	logrus.Debugf("copying blob %s from %s", layerDigest, ref.Exact())
	repo, err := restctx.RepositoryForRef(ctx, ref, insecure)
	if err != nil {
		return fmt.Errorf("create repo for %s: %v", ref, err)
	}
	dgst, err := digest.Parse(layerDigest)
	if err != nil {
		return err
	}
	rc, err := repo.Blobs(ctx).Open(ctx, dgst)
	if err != nil {
		return fmt.Errorf("open blob: %v", err)
	}
	defer rc.Close()
	for _, dstPath := range dstPaths {
		if err := copyBlobFile(rc, dstPath); err != nil {
			return fmt.Errorf("copy blob for %s: %v", ref, err)
		}
		if _, err := rc.Seek(0, 0); err != nil {
			return fmt.Errorf("seek to start of blob: %v", err)
		}
	}

	return nil
}

func unpack(archiveFilePath, dest string, filesInArchive map[string]string) error {
	name := filepath.Base(archiveFilePath)
	archivePath, found := filesInArchive[name]
	if !found {
		return &ErrArchiveFileNotFound{name}
	}
	if err := archive.NewArchiver().Extract(archivePath, archiveFilePath, dest); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dest, archiveFilePath)); err != nil {
		return err
	}
	return nil
}

func mktempDir(dir string) (func(), string, error) {
	dir, err := ioutil.TempDir(dir, "images.*")
	return func() {
		if err := os.RemoveAll(dir); err != nil {
			logrus.Fatal(err)
		}
	}, dir, err
}

// publishImages uses the `oc mirror` library to mirror generic images
func (o *MirrorOptions) publishImage(mappings []imgmirror.Mapping, fromDir string) error {
	var insecure bool
	if o.DestPlainHTTP || o.DestSkipTLS {
		insecure = true
	}
	// Mirror all file sources of each available image type to mirror registry.
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		var srcs []string
		for _, m := range mappings {
			srcs = append(srcs, m.Source.String())
		}
		logrus.Debugf("mirroring generic images: %q", srcs)
	}
	regctx, err := image.CreateDefaultContext(insecure)
	if err != nil {
		return err
	}
	genOpts := imgmirror.NewMirrorImageOptions(o.IOStreams)
	genOpts.Mappings = mappings
	genOpts.DryRun = o.DryRun
	genOpts.FromFileDir = fromDir
	// Filter must be a wildcard for publishing because we
	// cannot filter images within a catalog
	genOpts.FilterOptions = imagemanifest.FilterOptions{FilterByOS: ".*"}
	genOpts.SkipMultipleScopes = true
	genOpts.KeepManifestList = true
	genOpts.SecurityOptions.CachedContext = regctx
	genOpts.SecurityOptions.Insecure = insecure
	if err := genOpts.Validate(); err != nil {
		return fmt.Errorf("invalid image mirror options: %v", err)
	}
	if err := genOpts.Run(); err != nil {
		return fmt.Errorf("error running generic image mirror: %v", err)
	}

	return nil
}

func (o *MirrorOptions) findBlobRepo(blobs v1alpha2.Blobs, layerDigest string) (imagesource.TypedImageReference, error) {
	var namespacename string
	for _, blob := range blobs {
		if blob.ID == layerDigest {
			namespacename = blob.NamespaceName
			break
		}
	}
	if namespacename == "" {
		return imagesource.TypedImageReference{}, fmt.Errorf("layer %q is not present in previous metadata", layerDigest)
	}
	ref := path.Join(o.ToMirror, o.UserNamespace, namespacename)
	return imagesource.ParseReference(ref)
}
