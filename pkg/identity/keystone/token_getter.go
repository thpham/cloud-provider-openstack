/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package keystone

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	tokens3 "github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/v2/client"
	certutil "k8s.io/client-go/util/cert"
	osClient "k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog/v2"
)

type Options struct {
	AuthOptions    gophercloud.AuthOptions
	ClientCertPath string
	ClientKeyPath  string
	ClientCAPath   string
}

// GetToken creates a token by authenticate with keystone.
func GetToken(ctx context.Context, options Options) (*tokens3.Token, error) {
	var token *tokens3.Token
	var setTransport bool

	// Create new identity client
	provider, err := openstack.NewClient(options.AuthOptions.IdentityEndpoint)
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}
	tlsConfig := &tls.Config{}
	setTransport = false

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("client-keystone-auth/%s", version.Version))
	provider.UserAgent = userAgent

	if options.ClientCertPath != "" && options.ClientKeyPath != "" {
		clientCert, err := os.ReadFile(options.ClientCertPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read cert file: %v", err)
			return token, msg
		}

		clientKey, err := os.ReadFile(options.ClientKeyPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read key file: %v", err)
			return token, msg
		}

		cert, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
		if err != nil {
			msg := fmt.Errorf("failed: Cannot create keypair:: %v", err)
			return token, msg
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		setTransport = true
	}

	if options.ClientCAPath != "" {
		roots, err := certutil.NewPool(options.ClientCAPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read CA file: %v", err)
			return token, msg
		}

		tlsConfig.RootCAs = roots
		setTransport = true
	}

	if setTransport {
		transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig}
		provider.HTTPClient.Transport = transport
	}

	if klog.V(6).Enabled() {
		if provider.HTTPClient.Transport == nil {
			provider.HTTPClient.Transport = http.DefaultTransport
		}
		provider.HTTPClient.Transport = &client.RoundTripper{
			Rt:     provider.HTTPClient.Transport,
			Logger: &osClient.Logger{},
		}
	}

	v3Client, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}

	// Issue new unscoped token
	result := tokens3.Create(ctx, v3Client, &options.AuthOptions)
	if result.Err != nil {
		return token, result.Err
	}
	token, err = result.ExtractToken()
	if err != nil {
		msg := fmt.Errorf("failed: Cannot extract the token from the response")
		return token, msg
	}

	return token, nil
}
