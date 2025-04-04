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

package manila

import (
	"context"
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/snapshots"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

const (
	snapshotCreating  = "creating"
	snapshotDeleting  = "deleting"
	snapshotError     = "error"
	snapshotAvailable = "available"

	snapshotDescription = "snapshotted-by=manila.csi.openstack.org"
)

// getOrCreateSnapshot retrieves an existing snapshot with name=snapName, or creates a new one if it doesn't exist yet.
// Instead of waiting for the snapshot to become available (as getOrCreateShare does), CSI's ready_to_use flag is used to signal readiness
func getOrCreateSnapshot(ctx context.Context, manilaClient manilaclient.Interface, snapName, sourceShareID string) (*snapshots.Snapshot, error) {
	var (
		snapshot *snapshots.Snapshot
		err      error
	)

	// First, check if the snapshot already exists or needs to be created

	if snapshot, err = manilaClient.GetSnapshotByName(ctx, snapName); err != nil {
		if clouderrors.IsNotFound(err) {
			// It doesn't exist, create it

			opts := snapshots.CreateOpts{
				ShareID:     sourceShareID,
				Name:        snapName,
				Description: snapshotDescription,
			}

			var createErr error
			if snapshot, createErr = manilaClient.CreateSnapshot(ctx, opts); createErr != nil {
				return nil, createErr
			}

		} else {
			// Something else is wrong
			return nil, fmt.Errorf("failed to probe for a snapshot named %s: %v", snapName, err)
		}
	} else {
		klog.V(4).Infof("a snapshot named %s already exists", snapName)
	}

	return snapshot, nil
}

func deleteSnapshot(ctx context.Context, manilaClient manilaclient.Interface, snapID string) error {
	if err := manilaClient.DeleteSnapshot(ctx, snapID); err != nil {
		if clouderrors.IsNotFound(err) {
			klog.V(4).Infof("snapshot %s not found, assuming it to be already deleted", snapID)
		} else {
			return err
		}
	}

	return nil
}

func tryDeleteSnapshot(ctx context.Context, manilaClient manilaclient.Interface, snapshot *snapshots.Snapshot) {
	if snapshot == nil {
		return
	}

	if err := deleteSnapshot(ctx, manilaClient, snapshot.ID); err != nil {
		// TODO failure to delete a snapshot in an error state needs proper monitoring support
		klog.Errorf("couldn't delete snapshot %s in a roll-back procedure: %v", snapshot.ID, err)
		return
	}

	_, _, err := waitForSnapshotStatus(ctx, manilaClient, snapshot.ID, snapshotDeleting, "", true)
	if err != nil && !wait.Interrupted(err) {
		klog.Errorf("couldn't retrieve snapshot %s in a roll-back procedure: %v", snapshot.ID, err)
	}
}

func waitForSnapshotStatus(ctx context.Context, manilaClient manilaclient.Interface, snapshotID, currentStatus, desiredStatus string, successOnNotFound bool) (*snapshots.Snapshot, manilaError, error) {
	var (
		backoff = wait.Backoff{
			Duration: time.Second * waitForAvailableShareTimeout,
			Factor:   1.2,
			Steps:    waitForAvailableShareRetries,
		}

		snapshot      *snapshots.Snapshot
		manilaErrCode manilaError
		err           error
	)

	return snapshot, manilaErrCode, wait.ExponentialBackoff(backoff, func() (bool, error) {
		snapshot, err = manilaClient.GetSnapshotByID(ctx, snapshotID)

		if err != nil {
			if clouderrors.IsNotFound(err) && successOnNotFound {
				return true, nil
			}

			return false, err
		}

		var isAvailable bool

		switch snapshot.Status {
		case currentStatus:
			isAvailable = false
		case desiredStatus:
			isAvailable = true
		case shareError:
			manilaErrMsg, err := lastResourceError(ctx, manilaClient, snapshotID)
			if err != nil {
				return false, fmt.Errorf("snapshot %s is in error state, error description could not be retrieved: %v", snapshotID, err)
			}

			manilaErrCode = manilaErrMsg.errCode
			return false, fmt.Errorf("snapshot %s is in error state: %s", snapshotID, manilaErrMsg.message)
		default:
			return false, fmt.Errorf("snapshot %s is in an unexpected state: wanted either %s or %s, got %s", snapshotID, currentStatus, desiredStatus, snapshot.Status)
		}

		return isAvailable, nil
	})
}
