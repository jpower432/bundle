package config

import (
	"errors"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/openshift/oc-mirror/pkg/api/v1alpha2"
)

type validationFunc func(cfg *v1alpha2.ImageSetConfiguration) error

var validationChecks = []validationFunc{validateOperatorOptions}

func Validate(cfg *v1alpha2.ImageSetConfiguration) error {
	var errs []error
	for _, check := range validationChecks {
		if err := check(cfg); err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func validateOperatorOptions(cfg *v1alpha2.ImageSetConfiguration) error {
	for _, ctlg := range cfg.Mirror.Operators {
		if len(ctlg.IncludeConfig.Packages) != 0 && ctlg.IsHeadsOnly() {
			return errors.New(
				"invalid configuration option: catalog cannot define packages with headsOnly set to true",
			)
		}
	}
	return nil
}
