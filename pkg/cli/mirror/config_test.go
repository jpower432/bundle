package mirror

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/openshift/oc-mirror/pkg/cli"
	"github.com/openshift/oc-mirror/pkg/config/v1alpha1"
)

func TestAddOPMImage(t *testing.T) {

	var cfg v1alpha1.ImageSetConfiguration
	var meta v1alpha1.Metadata

	// No past OPMImage.
	cfg = v1alpha1.ImageSetConfiguration{}
	meta = v1alpha1.Metadata{}
	meta.MetadataSpec.PastMirrors = []v1alpha1.PastMirror{
		{
			Mirror: v1alpha1.Mirror{
				AdditionalImages: []v1alpha1.AdditionalImages{
					{Image: v1alpha1.Image{Name: "reg.com/ns/other:latest"}},
				},
			},
		},
	}

	addOPMImage(&cfg, meta)
	if assert.Len(t, cfg.Mirror.AdditionalImages, 1) {
		require.Equal(t, cfg.Mirror.AdditionalImages[0].Image.Name, OPMImage)
	}

	// Has past OPMImage.
	cfg = v1alpha1.ImageSetConfiguration{}
	meta = v1alpha1.Metadata{}
	meta.MetadataSpec.PastMirrors = []v1alpha1.PastMirror{
		{
			Mirror: v1alpha1.Mirror{
				AdditionalImages: []v1alpha1.AdditionalImages{
					{Image: v1alpha1.Image{Name: OPMImage}},
					{Image: v1alpha1.Image{Name: "reg.com/ns/other:latest"}},
				},
			},
		},
	}

	addOPMImage(&cfg, meta)
	require.Len(t, cfg.Mirror.AdditionalImages, 0)
}

func TestCreate(t *testing.T) {
	path := t.TempDir()
	ctx := context.Background()

	img := v1alpha1.AdditionalImages{
		Image: v1alpha1.Image{
			Name: "quay.io/redhatgov/oc-mirror-dev:latest",
		},
	}

	cfg := v1alpha1.ImageSetConfiguration{}
	cfg.Mirror.AdditionalImages = append(cfg.Mirror.AdditionalImages, img)

	opts := MirrorOptions{
		RootOptions: &cli.RootOptions{
			Dir:      path,
			LogLevel: "info",
			IOStreams: genericclioptions.IOStreams{
				In:     os.Stdin,
				Out:    os.Stdout,
				ErrOut: os.Stderr,
			},
		},
		OutputDir: path,
	}
	_, mappings, err := opts.FromConfig(ctx, cfg)
	require.NoError(t, err)
	// One mapping for OPM and one for the requested image
	require.Len(t, mappings, 2)
}
