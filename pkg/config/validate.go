package config

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/openshift/installer/pkg/validate"
	"github.com/openshift/library-go/pkg/image/registryclient"
	"github.com/openshift/oc-mirror/pkg/config/v1alpha1"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/sirupsen/logrus"
)

func CheckPermissions(ctx context.Context, restctx *registryclient.Context, skipTLS bool, ref, scope string) error {

	imgRef, err := imagesource.ParseReference(ref)
	if err != nil {
		return err
	}
	url, err := url.Parse(imgRef.Ref.Registry)
	if err != nil {
		return err
	}

	// Ping Registry
	rt, _, err := restctx.Ping(ctx, url, skipTLS)
	if err != nil {
		return err
	}

	// Validate pull secret
	_, password := restctx.Credentials.Basic(url)
	if err := validate.ImagePullSecret(password); err != nil {
		return err
	}

	client := &http.Client{Transport: rt}

	var auth bool
	switch scope {
	case "push":
		auth, err = checkPush(client, ctx, imgRef)
		if err != nil {
			return err
		}
		if err := cancel(client, url.Host); err != nil {
			return err
		}
	case "pull":
		auth, err = checkPull(client, ctx, imgRef)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("not a valid scope")
	}

	if !auth {
		return fmt.Errorf("not authenticated for scope %s", scope)
	}

	return nil
}

func ValidateSecret(cfg v1alpha1.ImageSetConfiguration) error {

	mirror := cfg.Mirror

	// Check OCP for validate pull secret
	if len(mirror.OCP.PullSecret) != 0 {
		logrus.Debug("Validating OCP secret")
		if err := validate.ImagePullSecret(mirror.OCP.PullSecret); err != nil {
			return fmt.Errorf("error validating OCP pullSecret: %v", err)
		}
	}

	// Check Operator for validate pull secret
	logrus.Debug("Validating operator secrets")
	for _, op := range mirror.Operators {
		if len(op.PullSecret) != 0 {
			if err := validate.ImagePullSecret(op.PullSecret); err != nil {
				return fmt.Errorf("error validating secret for operator catalog %s: %v", op.Catalog, err)
			}
		}
	}

	// Check Additional Images for validate pull secret
	logrus.Debug("Validating additional image secrets")
	for _, img := range mirror.AdditionalImages {
		if len(img.PullSecret) != 0 {
			if err := validate.ImagePullSecret(img.PullSecret); err != nil {
				return fmt.Errorf("error validating secret for image %s: %v", img, err)
			}
		}
	}

	return nil
}

func checkPull(client *http.Client, ctx context.Context, ref imagesource.TypedImageReference) (bool, error) {
	uri, err := url.Parse(fmt.Sprintf("%s/v2/%s/blobs/", ref.Ref.Registry, ref.Ref.AsRepository().AsRepository()))
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest(http.MethodHead, uri.String(), nil)
	if err != nil {
		return false, err
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func checkPush(client *http.Client, ctx context.Context, ref imagesource.TypedImageReference) (bool, error) {
	uri, err := url.Parse(fmt.Sprintf("%s/v2/%s/blobs/uploads/", ref.Ref.Registry, ref.Ref.AsRepository().AsRepository()))
	if err != nil {
		return false, err
	}
	queryParams := uri.Query()

	// For quay?
	queryParams.Add("mount", "mount")
	queryParams.Add("from", "from")
	uri.RawQuery = queryParams.Encode()

	// Make the request to initiate the blob upload.
	req, err := http.NewRequest(http.MethodPost, uri.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	// Check the response code to determine the result.
	switch resp.StatusCode {
	case http.StatusCreated:
		// We're done, we were able to fast-path.
		return true, nil
	case http.StatusAccepted:
		return true, nil
	default:
		return false, fmt.Errorf("failed")
	}
}

func cancel(client *http.Client, url string) error {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	_, err = client.Do(req)
	return err
}
