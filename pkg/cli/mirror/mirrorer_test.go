package mirror

import (
	"testing"

	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/stretchr/testify/require"
)

func TestImageBlocking(t *testing.T) {
	type fields struct {
		blockedImages []v1alpha2.Image
	}
	tests := []struct {
		name   string
		fields fields
		ref    string
		want   bool
	}{
		{
			name: "Success/ImageBlocked",
			fields: fields{
				blockedImages: []v1alpha2.Image{{Name: "alpine"}},
			},
			ref:  "docker.io/library/alpine:latest",
			want: true,
		},
		{
			name: "Success/ImageNotBlocked",
			fields: fields{
				blockedImages: []v1alpha2.Image{{Name: "alpine"}},
			},
			ref:  "registry.redhat.io/ubi8/ubi:latest",
			want: false,
		},
		{
			name: "Success/ImageNotBlockedContainsKeyword",
			fields: fields{
				blockedImages: []v1alpha2.Image{{Name: "alpine"}},
			},
			ref:  "docker.io/library/notalpine:latest",
			want: false,
		},
		{
			name: "Success/ImageBlockedNoTag",
			fields: fields{
				blockedImages: []v1alpha2.Image{{Name: "openshift-migration-velero-restic-restore-helper-rhel8"}},
			},
			ref:  "registry.redhat.io/rhmtc/openshift-migration-velero-restic-restore-helper-rhel8",
			want: true,
		},
	}
	for _, test := range tests {
		cfg := v1alpha2.ImageSetConfiguration{}
		cfg.Mirror = v1alpha2.Mirror{
			BlockedImages: test.fields.blockedImages,
		}

		img, err := imagesource.ParseReference(test.ref)
		require.NoError(t, err)

		actual := isBlocked(cfg.Mirror.BlockedImages, img.Ref)
		require.Equal(t, test.want, actual)
	}
}
