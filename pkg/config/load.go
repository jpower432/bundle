package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/openshift/oc-mirror/pkg/config/v1alpha2"
)

// TODO(estroz): create interface scheme such that configuration and metadata
// versions do not matter to the caller.
// See https://github.com/kubernetes-sigs/controller-runtime/blob/master/pkg/config/config.go

func LoadConfig(configPath string) (c v1alpha2.ImageSetConfiguration, err error) {

	data, err := ioutil.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return c, err
	}
	typeMeta, err := getTypeMeta(data)

	if err != nil {
		return c, err
	}

	switch typeMeta.GroupVersionKind() {
	case v1alpha2.GroupVersion.WithKind(v1alpha2.ImageSetConfigurationKind):
		c, err = v1alpha2.LoadConfig(data)
		if err != nil {
			return c, err
		}
	default:
		return c, fmt.Errorf("config GVK not recognized: %s", typeMeta.GroupVersionKind())
	}

	return c, Validate(&c)
}

func getTypeMeta(data []byte) (typeMeta metav1.TypeMeta, err error) {
	if err := yaml.Unmarshal(data, &typeMeta); err != nil {
		return typeMeta, fmt.Errorf("get type meta: %v", err)
	}
	return typeMeta, nil
}
