package image

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"k8s.io/klog/v2"
)

// TypedImage defines an a image with the destination and content type
type TypedImage struct {
	imagesource.TypedImageReference
	// Category adds image category type to TypedImageReference
	Category v1alpha2.ImageType
}

// ParseTypedImage will create a TypedImage from a string and type
func ParseTypedImage(image string, typ v1alpha2.ImageType) (TypedImage, error) {
	ref, err := imagesource.ParseReference(image)
	if err != nil {
		return TypedImage{}, err
	}
	t := TypedImage{ref, typ}
	return t.SetDefaults(), nil
}

// SetDefaults sets the default values for TypedImage fields
func (t TypedImage) SetDefaults() TypedImage {
	if len(t.Ref.Tag) == 0 {
		partial, err := getPartialDigest(t.Ref.ID)
		// If unable to get a partial digest
		// Set the tag to latest
		if err != nil {
			t.Ref.Tag = "latest"
		} else {
			t.Ref.Tag = partial
		}
	}
	return t
}

// TypedImageMapping is a mapping that contains a key,value pairs of
// image sources and destinations.
type TypedImageMapping map[TypedImage]TypedImage

// ToRegistry will convert all mapping values to a registry destination
func (m TypedImageMapping) ToRegistry(registry, namespace string) {
	for src, dest := range m {
		dest.Type = imagesource.DestinationRegistry
		dest.Ref.Registry = registry
		dest.Ref.Namespace = path.Join(namespace, dest.Ref.Namespace)
		dest.Ref.ID = src.Ref.ID
		dest = dest.SetDefaults()
		m[src] = dest
	}
}

// Merge will add new image maps to current map
func (m TypedImageMapping) Merge(in TypedImageMapping) {
	for k, v := range in {
		_, found := m[k]
		if found {
			klog.V(4).Infof("source image %s already exists in mapping", k.String())
			continue
		}
		m[k] = v
	}
}

// Add stores a key-value pair into image map
func (m TypedImageMapping) Add(srcRef, dstRef imagesource.TypedImageReference, typ v1alpha2.ImageType) {
	srcTypedRef := TypedImage{
		TypedImageReference: srcRef,
		Category:            typ,
	}
	dstTypedRef := TypedImage{
		TypedImageReference: dstRef,
		Category:            typ,
	}
	m[srcTypedRef] = dstTypedRef
}

// Remove will remove a TypedImage from the mapping
func (m TypedImageMapping) Remove(images ...TypedImage) {
	for _, img := range images {
		delete(m, img)
	}
}

// ByCategory will return a pruned mapping containing provided types
func ByCategory(m TypedImageMapping, types ...v1alpha2.ImageType) TypedImageMapping {
	foundTypes := map[v1alpha2.ImageType]struct{}{}
	for _, typ := range types {
		foundTypes[typ] = struct{}{}
	}
	// return a new map with the pruned mapping
	prunedMap := TypedImageMapping{}
	for key, val := range m {
		_, ok := foundTypes[key.Category]
		if ok {
			prunedMap[key] = val
		}
	}
	return prunedMap
}

// ReadImageMapping reads a mapping.txt file and parses each line into a map k/v.
func ReadImageMapping(mappingsPath, separator string, typ v1alpha2.ImageType) (TypedImageMapping, error) {
	f, err := os.Open(filepath.Clean(mappingsPath))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mappings := TypedImageMapping{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		text := scanner.Text()
		split := strings.Split(text, separator)
		if len(split) != 2 {
			return nil, fmt.Errorf("mapping %q expected to have exactly one \"%s\"", separator, text)
		}
		srcTypedRef, err := ParseTypedImage(strings.TrimSpace(split[0]), typ)
		if err != nil {
			return nil, err
		}
		dstTypedRef, err := ParseTypedImage(strings.TrimSpace(split[1]), typ)
		if err != nil {
			return nil, err
		}
		mappings[srcTypedRef] = dstTypedRef
	}

	return mappings, scanner.Err()
}

// WriteImageMapping writes key map k/v to a mapping.txt file.
func WriteImageMapping(m TypedImageMapping, mappingsPath string) error {
	f, err := os.Create(filepath.Clean(mappingsPath))
	if err != nil {
		return err
	}
	defer f.Close()
	for fromStr, toStr := range m {
		_, err := f.WriteString(fmt.Sprintf("%s=%s\n", fromStr.Ref.Exact(), toStr.Ref.Exact()))
		if err != nil {
			return err
		}
	}
	return nil
}
