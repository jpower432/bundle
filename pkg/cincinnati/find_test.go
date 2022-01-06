package cincinnati

import (
	"testing"

	"github.com/blang/semver/v4"
	"github.com/openshift/oc-mirror/pkg/config/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestFindLatestRelease(t *testing.T) {
	channelName := "test-channel"

	tests := []struct {
		name string

		expectedVer  semver.Version
		expectedChan string
		channels     []v1alpha1.ReleaseChannel
		err          string
	}{{
		name: "two previous releases",
		channels: []v1alpha1.ReleaseChannel{
			{
				Name:     channelName,
				Versions: []string{"4.0.0-5"},
			},
			{
				Name:     "another-channel",
				Versions: []string{"4.0.0-6"},
			},
		},
		expectedVer:  semver.MustParse("4.0.0-6"),
		expectedChan: "another-channel",
	}, {
		name: "one previous release in another channel",
		channels: []v1alpha1.ReleaseChannel{
			{
				Name:     "another-channel",
				Versions: []string{"4.0.0-5"},
			},
		},
		expectedVer:  semver.MustParse("4.0.0-5"),
		expectedChan: "another-channel",
	}, {
		name:     "no previous release",
		channels: []v1alpha1.ReleaseChannel{},
		err:      ErrNoPreviousRelease.Error(),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			meta := v1alpha1.Metadata{
				MetadataSpec: v1alpha1.MetadataSpec{
					PastMirrors: []v1alpha1.PastMirror{
						{
							Mirror: v1alpha1.Mirror{
								OCP: v1alpha1.OCP{
									Graph:    false,
									Channels: test.channels,
								},
							},
						},
					},
				},
			}

			ch, ver, err := FindLastRelease(meta)

			if len(test.err) != 0 {
				require.Equal(t, err.Error(), test.err)
			} else {
				if !ver.EQ(test.expectedVer) {
					t.Errorf("Test failed. Expected %s, got %s", test.expectedVer.String(), ver.String())
				}
				if ch != test.expectedChan {
					t.Errorf("Test failed. Expected %s, got %s", test.expectedChan, ch)
				}
			}
		})
	}
}
