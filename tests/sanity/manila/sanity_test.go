/*
Copyright 2019 The Kubernetes Authors.

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

package sanity

import (
	"os"
	"path"
	"testing"

	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila"
)

func TestDriver(t *testing.T) {
	basePath := os.TempDir()
	defer os.Remove(basePath)

	endpoint := path.Join(basePath, "csi.sock")
	fwdEndpoint := "unix:///fake-fwd-endpoint"

	d, err := manila.NewDriver(
		&manila.DriverOpts{
			DriverName:          "fake.manila.csi.openstack.org",
			WithTopology:        true,
			ShareProto:          "NFS",
			ServerCSIEndpoint:   endpoint,
			FwdCSIEndpoint:      fwdEndpoint,
			ManilaClientBuilder: &fakeManilaClientBuilder{},
			CSIClientBuilder:    &fakeCSIClientBuilder{},
		},
	)
	if err != nil {
		t.Fatalf("Failed to initialize CSI Manila driver: %v", err)
	}

	err = d.SetupControllerService()
	if err != nil {
		t.Fatalf("Failed to initialize CSI Manila controller service: %v", err)
	}

	fakemeta := &fakemetadata{}

	err = d.SetupNodeService(fakemeta)
	if err != nil {
		t.Fatalf("Failed to initialize CSI Manila node service: %v", err)
	}

	go d.Run()

	config := sanity.NewTestConfig()
	config.Address = endpoint
	config.SecretsFile = "fake-secrets.yaml"
	sanity.Test(t, config)

}
