package mirror

import (
	"fmt"

	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
	"github.com/sirupsen/logrus"
)

// Mirrorer
type Mirrorer interface {
	Mirror(MirrorOptions) error
}

var _ Mirrorer = &fromDiskStrategy{}

type fromDiskStrategy struct {
	sourceDir string
}

func (m *fromDiskStrategy) Mirror(opts MirrorOptions) error {
	return nil
}

var _ Mirrorer = &fromRegistryStrategy{}

type fromRegistryStrategy struct {
	blockedImages []string
}

func (m *fromRegistryStrategy) Mirror(opts MirrorOptions) error {
	return nil
}

type ErrBlocked struct {
	image string
}

func (e ErrBlocked) Error() string {
	return fmt.Sprintf("image %s blocked", e.image)
}

// IsBlocked will return a boolean value on whether an image
// is specified as blocked in the ImageSetConfigSpec
func isBlocked(blocked []v1alpha2.Image, imgRef reference.DockerImageReference) bool {

	for _, block := range blocked {

		logrus.Debugf("Checking if image %s is blocked", imgRef.Exact())

		if imgRef.Name == block.Name {
			return true
		}
	}
	return false
}
