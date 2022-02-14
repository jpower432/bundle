package cincinnati

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

type Client interface {
	GetURL() *url.URL
	SetQueryParams(arch, channel, version string)
	GetID() uuid.UUID
	GetTransport() *http.Transport
}

var _ Client = &ocpClient{}

type ocpClient struct {
	id        uuid.UUID
	transport *http.Transport
	url       url.URL
}

var _ Client = &okdClient{}

type okdClient struct {
	id        uuid.UUID
	transport *http.Transport
	url       url.URL
}

// NewReleaseClient creates a new Cincinnati client with the given client identifier.
func NewOCPClient(id uuid.UUID) (Client, error) {
	upstream, err := url.Parse(UpdateUrl)
	if err != nil {
		return &ocpClient{}, err
	}

	tls, err := getTLSConfig()
	if err != nil {
		return &ocpClient{}, err
	}

	transport := &http.Transport{
		TLSClientConfig: tls,
		Proxy:           http.ProxyFromEnvironment,
	}
	return &ocpClient{id: id, transport: transport, url: *upstream}, nil
}

// NewReleaseClient creates a new Cincinnati client with the given client identifier.
func NewOKDClient(id uuid.UUID) (Client, error) {
	upstream, err := url.Parse(OkdUpdateURL)
	if err != nil {
		return &okdClient{}, err
	}

	tls, err := getTLSConfig()
	if err != nil {
		return &okdClient{}, err
	}

	transport := &http.Transport{
		TLSClientConfig: tls,
		Proxy:           http.ProxyFromEnvironment,
	}
	return &okdClient{id: id, transport: transport, url: *upstream}, nil
}

func getTLSConfig() (*tls.Config, error) {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		RootCAs: certPool,
	}
	return config, nil
}

func (c *ocpClient) GetURL() *url.URL {
	return &c.url
}

func (c *ocpClient) GetTransport() *http.Transport {
	return c.transport
}

func (c *ocpClient) GetID() uuid.UUID {
	return c.id
}

func (c *ocpClient) SetQueryParams(arch, channel, version string) {
	queryParams := c.url.Query()
	queryParams.Add("id", c.id.String())
	params := map[string]string{
		"arch":    arch,
		"channel": channel,
		"version": version,
	}
	for key, value := range params {
		if value != "" {
			queryParams.Add(key, value)
		}
	}
	c.url.RawQuery = queryParams.Encode()
}

func (c *okdClient) GetURL() *url.URL {
	return &c.url
}

func (c *okdClient) GetID() uuid.UUID {
	return c.id
}

func (c *okdClient) GetTransport() *http.Transport {
	return c.transport
}

func (c *okdClient) SetQueryParams(_, _, _ string) {
	// Do nothing
}
