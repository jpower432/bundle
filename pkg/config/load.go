package config

import (
	"fmt"
	"io/ioutil"
	"sort"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
)

// ImageSetConfiguration defines the functions necessary to parse a config file
// and to configure the Options struct for the ctrl.Manager.
type ImageSetConfiguration interface {
	runtime.Object
	// Complete returns the versioned configuration
	Complete() (v1alpha2.ImageSetConfigurationSpec, error)
}

type Metadata interface {
	runtime.Object
	// Complete returns the versioned configuration
	Complete() (v1alpha2.MetadataSpec, error)
}

// DeferredFileLoader is used to configure the decoder for loading controller
// runtime component config types.
type Loader struct {
	path   string
	inline []byte
	scheme *runtime.Scheme
	once   sync.Once
	err    error
}

type ConfigLoader struct {
	ImageSetConfiguration
	Loader
}

type MetadataLoader struct {
	Metadata
	Loader
}

func Config(l Loader) *ConfigLoader {
	if l.path == "" {
		l.path = "./imageset-config.yaml"
	}
	return &ConfigLoader{
		Loader:                l,
		ImageSetConfiguration: &v1alpha2.ImageSetConfiguration{},
	}
}

func NewLoader() *Loader {
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	return &Loader{
		inline: []byte{},
		path:   "",
		scheme: scheme,
	}
}

func Meta(l Loader) *MetadataLoader {
	return &MetadataLoader{
		Loader:   l,
		Metadata: &v1alpha2.Metadata{},
	}
}

// Complete will use sync.Once to set the scheme.
func (c *ConfigLoader) Complete() (v1alpha2.ImageSetConfigurationSpec, error) {
	c.once.Do(c.loadData)
	if c.err != nil {
		return v1alpha2.ImageSetConfigurationSpec{}, c.err
	}
	return c.ImageSetConfiguration.Complete()
}

// Complete will use sync.Once to set the scheme.
func (c *MetadataLoader) Complete() (v1alpha2.MetadataSpec, error) {
	c.once.Do(c.loadData)
	if c.err != nil {
		return v1alpha2.MetadataSpec{}, c.err
	}
	return c.Metadata.Complete()
}

// AtPath will set the path to load the file for the decoder.
func (l *Loader) AtPath(path string) *Loader {
	l.path = path
	return l
}

// WithContent will set the inline content for the decoder.
func (l *Loader) WithContent(content []byte) *Loader {
	l.inline = content
	return l
}

// OfKind will set the type to be used for decoding the file into.
func (c *ConfigLoader) OfKind(obj ImageSetConfiguration) *ConfigLoader {
	c.ImageSetConfiguration = obj
	return c
}

// OfKind will set the type to be used for decoding the file into.
func (m *MetadataLoader) OfKind(obj Metadata) *MetadataLoader {
	m.Metadata = obj
	return m
}

// InjectScheme will configure the scheme to be used for decoding the file.
func (l *Loader) InjectScheme(scheme *runtime.Scheme) error {
	l.scheme = scheme
	return nil
}

// loadData is used from the mutex.Once to load the file.
func (c *ConfigLoader) loadData() {
	if c.scheme == nil {
		c.err = fmt.Errorf("scheme not supplied to imageset configuration loader")
		return
	}

	var content []byte
	var err error
	if len(c.inline) != 0 {
		content = c.inline
	} else {
		content, err = ioutil.ReadFile(c.path)
		if err != nil {
			c.err = fmt.Errorf("could not read file at %s", c.path)
			return
		}

	}

	codecs := serializer.NewCodecFactory(c.scheme)

	// Regardless of if the bytes are of any external version,
	// it will be read successfully and converted into the internal version
	if err = runtime.DecodeInto(codecs.UniversalDecoder(), content, c.ImageSetConfiguration); err != nil {
		c.err = fmt.Errorf("could not decode file into runtime.Object")
	}
}

// loadData is used from the mutex.Once to load the file.
func (m *MetadataLoader) loadData() {
	if m.scheme == nil {
		m.err = fmt.Errorf("scheme not supplied to imageset configuration loader")
		return
	}

	var content []byte
	var err error
	if len(m.inline) != 0 {
		content = m.inline
	} else {
		content, err = ioutil.ReadFile(m.path)
		if err != nil {
			m.err = fmt.Errorf("could not read file at %s", m.path)
			return
		}

	}

	codecs := serializer.NewCodecFactory(m.scheme)

	// Regardless of if the bytes are of any external version,
	// it will be read successfully and converted into the internal version
	if err = runtime.DecodeInto(codecs.UniversalDecoder(), content, m.Metadata); err != nil {
		m.err = fmt.Errorf("could not decode file into runtime.Object")
	}
}

func LoadConfig(configPath string) (v1alpha2.ImageSetConfigurationSpec, error) {
	loader := NewLoader().AtPath(configPath)
	conf := v1alpha2.ImageSetConfiguration{}
	configLoader := Config(*loader).OfKind(&conf)
	return configLoader.Complete()
}

func LoadMetadata(data []byte) (v1alpha2.Metadata, error) {
	meta := v1alpha2.NewMetadata()
	loader := NewLoader().WithContent(data)
	metaLoader := Meta(*loader).OfKind(&meta)
	m, err := metaLoader.Complete()
	if err != nil {
		return meta, err
	}

	// Make sure blobs are sorted by timestamp
	sort.Sort(sort.Reverse(m.PastMirror.Blobs))

	meta.MetadataSpec = m
	return meta, nil
}
