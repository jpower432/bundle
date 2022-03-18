package config

import (
	"testing"

	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {

	type spec struct {
		name     string
		config   *v1alpha2.ImageSetConfiguration
		expError string
	}

	cases := []spec{
		{
			name: "Valid/HeadsOnlyFalse",
			config: &v1alpha2.ImageSetConfiguration{
				ImageSetConfigurationSpec: v1alpha2.ImageSetConfigurationSpec{
					Mirror: v1alpha2.Mirror{
						Operators: []v1alpha2.Operator{
							{
								IncludeConfig: v1alpha2.IncludeConfig{
									Packages: []v1alpha2.IncludePackage{{Name: "foo"}},
								},
								AllPackages: true,
							},
						},
					},
				},
			},
			expError: "",
		},
		{
			name: "Valid/NoIncludePackages",
			config: &v1alpha2.ImageSetConfiguration{
				ImageSetConfigurationSpec: v1alpha2.ImageSetConfigurationSpec{
					Mirror: v1alpha2.Mirror{
						Operators: []v1alpha2.Operator{
							{
								IncludeConfig: v1alpha2.IncludeConfig{},
								AllPackages:   false,
							},
						},
					},
				},
			},
		},
		{
			name: "Valid/HeadsOnlyFalse",
			config: &v1alpha2.ImageSetConfiguration{
				ImageSetConfigurationSpec: v1alpha2.ImageSetConfigurationSpec{
					Mirror: v1alpha2.Mirror{
						Operators: []v1alpha2.Operator{
							{
								IncludeConfig: v1alpha2.IncludeConfig{
									Packages: []v1alpha2.IncludePackage{{Name: "foo"}},
								},
								AllPackages: true,
							},
						},
					},
				},
			},
		},
		{
			name: "Invalid/HeadsOnlyTrue",
			config: &v1alpha2.ImageSetConfiguration{
				ImageSetConfigurationSpec: v1alpha2.ImageSetConfigurationSpec{
					Mirror: v1alpha2.Mirror{
						Operators: []v1alpha2.Operator{
							{
								IncludeConfig: v1alpha2.IncludeConfig{
									Packages: []v1alpha2.IncludePackage{{Name: "foo"}},
								},
								AllPackages: false,
							},
						},
					},
				},
			},
			expError: "invalid configuration option: catalog cannot define packages with headsOnly set to true",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.config)
			if c.expError != "" {
				require.EqualError(t, err, c.expError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
