package mirror

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	semver "github.com/blang/semver/v4"
	"github.com/google/uuid"
	"github.com/openshift/oc/pkg/cli/admin/release"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"

	"github.com/openshift/oc-mirror/pkg/bundle"
	"github.com/openshift/oc-mirror/pkg/cincinnati"
	"github.com/openshift/oc-mirror/pkg/config"
	"github.com/openshift/oc-mirror/pkg/config/v1alpha1"
	"github.com/openshift/oc-mirror/pkg/image"
)

var supportedArchs = []string{"amd64", "ppc64le", "s390x"}

// archMap maps Go architecture strings to OpenShift supported values for any that differ.
var archMap = map[string]string{
	"amd64": "x86_64",
}

// ReleaseOptions configures either a Full or Diff mirror operation
// on a particular release image.
type ReleaseOptions struct {
	*MirrorOptions
	release string
	arch    []string
	uuid    uuid.UUID
}

// NewReleaseOptions defaults ReleaseOptions.
func NewReleaseOptions(mo *MirrorOptions, flags *pflag.FlagSet) *ReleaseOptions {
	var arch []string
	opts := mo.FilterOptions
	opts.Complete(flags)
	if opts.IsWildcardFilter() {
		arch = supportedArchs
	} else {
		arch = []string{strings.Join(strings.Split(opts.FilterByOS, "/")[1:], "/")}
	}

	return &ReleaseOptions{
		MirrorOptions: mo,
		arch:          arch,
		uuid:          uuid.New(),
	}
}

// GetReleases will pill release payloads based on user configuration
func (o *ReleaseOptions) GetReleases(ctx context.Context, meta v1alpha1.Metadata, cfg *v1alpha1.ImageSetConfiguration) (image.AssociationSet, error) {

	var (
		pullSecret       = cfg.Mirror.OCP.PullSecret
		srcDir           = filepath.Join(o.Dir, config.SourceDir)
		channelVersion   = make(map[string]string, len(cfg.Mirror.OCP.Channels))
		releaseDownloads = downloads{}
	)

	for _, ch := range cfg.Mirror.OCP.Channels {

		uri := cincinnati.UpdateUrl
		if ch.Name == "okd" {
			uri = cincinnati.OkdUpdateURL
		}

		client, upstream, err := cincinnati.NewClient(uri, o.uuid)
		if err != nil {
			return nil, err
		}
		for _, arch := range o.arch {
			if len(ch.Versions) == 0 {
				// If no version was specified from the channel, then get the latest release
				latest, err := client.GetChannelLatest(ctx, upstream, arch, ch.Name)
				if err != nil {
					return nil, err
				}
				// Update version to release channel
				ch.Versions = append(ch.Versions, latest.String())
				channelVersion[ch.Name] = latest.String()
			}
			// Check for specific version declarations for each specific version
			for _, v := range ch.Versions {

				downloads, err := o.getDownloads(ctx, client, meta, v, ch.Name, arch, upstream)
				if err != nil {
					return nil, err
				}
				releaseDownloads.Merge(downloads)
			}
		}
	}

	assocs, err := o.mirror([]byte(pullSecret), srcDir, releaseDownloads)
	if err != nil {
		return nil, err
	}

	// Update cfg release channels with latest versions
	// if applicable
	cfg.Mirror.OCP.Channels = updateReleaseChannel(cfg.Mirror.OCP.Channels, channelVersion)

	return assocs, nil
}

// getDownloads will prepare the downloads map for mirroring
func (o *ReleaseOptions) getDownloads(ctx context.Context, client cincinnati.Client, meta v1alpha1.Metadata, version, channel, arch string, url *url.URL) (downloads, error) {
	downloads := map[string]download{}

	requested, err := semver.Parse(version)
	if err != nil {
		return nil, err
	}

	// If no release has been downloaded for the
	// channel, download the requested version
	lastCh, lastVer, err := cincinnati.FindLastRelease(meta)
	currCh := channel
	reverse := false
	logrus.Infof("Downloading requested release %s", requested.String())
	switch {
	case err != nil && errors.Is(err, cincinnati.ErrNoPreviousRelease):
		lastVer = requested
		lastCh = channel
	case err != nil:
		return nil, err
	case requested.LT(lastVer):
		logrus.Debugf("Found current release %s", lastVer.String())
		// If the requested version is an earlier release than previous
		// downloads switch the values to get updates between the
		// later and earlier version
		currCh = lastCh
		lastCh = channel
		requested = lastVer
		lastVer = semver.MustParse(version)
		// Download the current image since this will not be in the updates
		reverse = true
	default:
		logrus.Debugf("Found current release %s", lastVer.String())
	}

	// This dumps the available upgrades from the last downloaded version
	current, newest, updates, err := client.CalculateUpgrades(ctx, url, arch, lastCh, currCh, lastVer, requested)
	if err != nil {
		return nil, fmt.Errorf("failed to get upgrade graph: %v", err)
	}

	for _, update := range updates {
		download := download{
			Update: update,
			arch:   arch,
		}
		downloads[update.Image] = download
	}

	// If reverse graph download the current version
	// else add newest to downloads
	if reverse {
		download := download{
			Update: current,
			arch:   arch,
		}
		downloads[current.Image] = download
		// Remove newest from updates as it has already
		// been downloaded
		delete(downloads, newest.Image)
	} else {
		download := download{
			Update: newest,
			arch:   arch,
		}
		downloads[newest.Image] = download
	}

	return downloads, nil
}

// mirror will take the prepare download information and mirror to disk location
func (o *ReleaseOptions) mirror(secret []byte, toDir string, downloads map[string]download) (image.AssociationSet, error) {
	allAssocs := image.AssociationSet{}

	for img, download := range downloads {
		logrus.Debugf("Starting release download for version %s", download.Version.String())
		opts := release.NewMirrorOptions(o.IOStreams)
		opts.ToDir = toDir

		// If the pullSecret is not empty create a cached context
		// else let `oc mirror` use the default docker config location
		if len(secret) != 0 {
			ctx, err := config.CreateContext(secret, o.SkipVerification, o.SourceSkipTLS)
			if err != nil {
				return nil, err
			}
			opts.SecurityOptions.CachedContext = ctx
		}

		opts.SecurityOptions.Insecure = o.SourceSkipTLS
		opts.SecurityOptions.SkipVerification = o.SkipVerification
		opts.DryRun = o.DryRun
		opts.From = img
		if err := opts.Run(); err != nil {
			return nil, err
		}

		// Do not build associations on dry runs because there are no manifests
		if !o.DryRun {
			// Retrive the mapping information for release
			mapping, images, err := o.getMapping(*opts, download.arch, download.Version.String())

			if err != nil {
				return nil, fmt.Errorf("error could not retrieve mapping information: %v", err)
			}

			logrus.Debugln("starting image association")
			assocs, err := image.AssociateImageLayers(toDir, mapping, images, image.TypeOCPRelease)
			if err != nil {
				return nil, err
			}

			// Check if a release image was provided with mapping
			if o.release == "" {
				return nil, errors.New("release image not found in mapping")
			}

			// Update all images associated with a release to the
			// release images so they form one keyset for publising
			for _, img := range images {
				if err := assocs.UpdateKey(img, o.release); err != nil {
					return nil, err
				}
			}

			allAssocs.Merge(assocs)
		}
	}

	return allAssocs, nil
}

// getMapping will run release mirror with ToMirror set to true to get mapping information
func (o *ReleaseOptions) getMapping(opts release.MirrorOptions, arch, version string) (mappings map[string]string, images []string, err error) {

	mappingPath := filepath.Join(o.Dir, "release-mapping.txt")
	file, err := os.Create(mappingPath)
	defer os.Remove(mappingPath)
	if err != nil {
		return mappings, images, err
	}
	defer file.Close()

	// Run release mirror with ToMirror set to retrieve mapping information
	// store in buffer for manipulation before outputting to mapping.txt
	var buffer bytes.Buffer
	opts.IOStreams.Out = &buffer
	opts.ToMirror = true

	if err := opts.Run(); err != nil {
		return mappings, images, err
	}

	newArch, found := archMap[arch]
	if found {
		arch = newArch
	}

	scanner := bufio.NewScanner(&buffer)

	// Scan mapping output and write to file
	for scanner.Scan() {
		text := scanner.Text()
		idx := strings.LastIndex(text, " ")
		if idx == -1 {
			return nil, nil, fmt.Errorf("invalid mapping information for release %v", version)
		}
		srcRef := text[:idx]
		// Get release image name from mapping
		// Only the top release need to be resolve because all other image key associated to the
		// will be updated to this value
		//
		// afflom - Select on ocp-release OR origin
		if strings.Contains(srcRef, "ocp-release") || strings.Contains(srcRef, "origin/release") {
			if !image.IsImagePinned(srcRef) {
				srcRef, err = bundle.PinImages(context.TODO(), srcRef, "", o.SourceSkipTLS)
			}
			o.release = srcRef
		}

		// Generate name of target directory
		dstRef := opts.TargetFn(text[idx+1:]).Exact()
		nameIdx := strings.LastIndex(dstRef, version)
		if nameIdx == -1 {
			return nil, nil, fmt.Errorf("image missing version %s for image %q", version, srcRef)
		}
		img := dstRef[nameIdx+len(version):]
		img = strings.TrimPrefix(img, "-")
		names := []string{version, arch}
		if img != "" {
			names = append(names, img)
		}
		dstRef = strings.Join(names, "-")

		// Append mapping file
		if _, err := file.WriteString(srcRef + "=file://openshift/release:" + dstRef + "\n"); err != nil {
			return mappings, images, err
		}

		images = append(images, srcRef)
	}

	mappings, err = image.ReadImageMapping(mappingPath)

	if err != nil {
		return mappings, images, err
	}

	return mappings, images, nil
}

// updateReleaseChannel will add a version to the ReleaseChannel to record
// for metadata
func updateReleaseChannel(releaseChannels []v1alpha1.ReleaseChannel, channelVersions map[string]string) []v1alpha1.ReleaseChannel {
	for i, ch := range releaseChannels {
		v, found := channelVersions[ch.Name]
		if found {
			releaseChannels[i].Versions = append(releaseChannels[i].Versions, v)
		}
	}
	return releaseChannels
}

// Define download types
type downloads map[string]download
type download struct {
	cincinnati.Update
	arch string
}

func (d downloads) Merge(in downloads) {
	for k, v := range in {
		d[k] = v
	}
}
