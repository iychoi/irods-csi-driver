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

/*
Copyright 2020 CyVerse
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

package driver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
)

var (
	nodeCaps = []csi.NodeServiceCapability_RPC_Type{}
)

const (
	sensitiveArgsRemoved = "<masked>"
)

// NodeStageVolume handles persistent volume stage event in node service
func (driver *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeUnstageVolume handles persistent volume unstage event in node service
func (driver *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodePublishVolume handles persistent volume publish event in node service
func (driver *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	//klog.V(4).Infof("NodePublishVolume: called with args %+v", req)

	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	klog.V(4).Infof("NodePublishVolume: volumeId (%#v)", volID)

	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	if !driver.isValidVolumeCapabilities([]*csi.VolumeCapability{volCap}) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}

	mountOptions := []string{}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	if m := volCap.GetMount(); m != nil {
		hasOption := func(options []string, opt string) bool {
			for _, o := range options {
				if o == opt {
					return true
				}
			}
			return false
		}
		for _, f := range m.MountFlags {
			if !hasOption(mountOptions, f) {
				mountOptions = append(mountOptions, f)
			}
		}
	}

	volContext := req.GetVolumeContext()
	volSecrets := req.GetSecrets()

	irodsClient := ExtractIRODSClientType(volContext, FuseType)

	klog.V(5).Infof("NodePublishVolume: creating dir %s", targetPath)
	if err := MakeDir(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not create dir %q: %v", targetPath, err)
	}

	switch irodsClient {
	case FuseType:
		klog.V(5).Infof("NodePublishVolume: mounting %s", irodsClient)
		if err := driver.mountFuse(volContext, volSecrets, mountOptions, targetPath); err != nil {
			os.Remove(targetPath)
			return nil, err
		}
	case WebdavType:
		klog.V(5).Infof("NodePublishVolume: mounting %s", irodsClient)
		if err := driver.mountWebdav(volContext, volSecrets, mountOptions, targetPath); err != nil {
			os.Remove(targetPath)
			return nil, err
		}
	case NfsType:
		klog.V(5).Infof("NodePublishVolume: mounting %s", irodsClient)
		if err := driver.mountNfs(volContext, volSecrets, mountOptions, targetPath); err != nil {
			os.Remove(targetPath)
			return nil, err
		}
	default:
		os.Remove(targetPath)
		return nil, status.Errorf(codes.Internal, "unknown driver type - %v", irodsClient)
	}

	klog.V(5).Infof("NodePublishVolume: %s was mounted", targetPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume handles persistent volume unpublish event in node service
func (driver *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	//klog.V(4).Infof("NodeUnpublishVolume: called with args %+v", req)

	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	klog.V(4).Infof("NodeUnpublishVolume: volumeId (%#v)", volID)

	target := req.GetTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}

	// Check if target directory is a mount point. GetDeviceNameFromMount
	// given a mnt point, finds the device from /proc/mounts
	// returns the device name, reference count, and error code
	_, refCount, err := driver.mounter.GetDeviceName(target)
	if err != nil {
		msg := fmt.Sprintf("failed to check if volume is mounted: %v", err)
		return nil, status.Error(codes.Internal, msg)
	}

	// From the spec: If the volume corresponding to the volume_id
	// is not staged to the staging_target_path, the Plugin MUST
	// reply 0 OK.
	if refCount == 0 {
		klog.V(5).Infof("NodeUnpublishVolume: %s target not mounted", target)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	klog.V(5).Infof("NodeUnpublishVolume: unmounting %s", target)
	err = driver.mounter.Unmount(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not unmount %q: %v", target, err)
	}
	klog.V(5).Infof("NodeUnpublishVolume: %s unmounted", target)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetVolumeStats returns volume stats
func (driver *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeExpandVolume expands volume
func (driver *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeGetCapabilities returns capabilities
func (driver *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(4).Infof("NodeGetCapabilities: called with args %+v", req)
	var caps []*csi.NodeServiceCapability
	for _, cap := range nodeCaps {
		c := &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

// NodeGetInfo returns node info
func (driver *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(4).Infof("NodeGetInfo: called with args %+v", req)

	return &csi.NodeGetInfoResponse{
		NodeId: driver.config.NodeID,
	}, nil
}

func (driver *Driver) mountFuse(volContext map[string]string, volSecrets map[string]string, mntOptions []string, targetPath string) error {
	irodsConn, err := ExtractIRODSConnection(volContext, volSecrets)
	if err != nil {
		return err
	}

	ticket := ""

	for k, v := range volContext {
		switch strings.ToLower(k) {
		case "ticket":
			// ticket is optional
			ticket = v
		default:
			return status.Error(codes.InvalidArgument, fmt.Sprintf("Volume context property %s not supported", k))
		}
	}

	for k, v := range volSecrets {
		switch strings.ToLower(k) {
		case "ticket":
			// ticket is optional
			ticket = v
		default:
			// ignore
			continue
		}
	}

	fsType := "irodsfs"
	source := fmt.Sprintf("irods://%s@%s:%d/%s/%s", irodsConn.User, irodsConn.Hostname, irodsConn.Port, irodsConn.Zone, strings.Trim(irodsConn.Path, "/"))

	mountOptions := []string{}
	mountSensitiveOptions := []string{}
	stdinArgs := []string{}

	mountOptions = append(mountOptions, mntOptions...)

	if len(ticket) > 0 {
		mountSensitiveOptions = append(mountSensitiveOptions, fmt.Sprintf("ticket=%s", ticket))
	}

	stdinArgs = append(stdinArgs, irodsConn.Password)

	klog.V(5).Infof("Mounting %s (%s) at %s with options %v", source, fsType, targetPath, mountOptions)
	if err := driver.mounter.MountSensitive2(source, source, targetPath, fsType, mountOptions, mountSensitiveOptions, stdinArgs); err != nil {
		return status.Errorf(codes.Internal, "Could not mount %q (%q) at %q: %v", source, fsType, targetPath, err)
	}

	return nil
}

func (driver *Driver) mountWebdav(volContext map[string]string, volSecrets map[string]string, mntOptions []string, targetPath string) error {
	irodsConn, err := ExtractIRODSWebDAVConnection(volContext, volSecrets)
	if err != nil {
		return err
	}

	fsType := "davfs"
	source := irodsConn.URL

	mountOptions := []string{}
	mountSensitiveOptions := []string{}
	stdinArgs := []string{}

	mountOptions = append(mountOptions, mntOptions...)

	if len(irodsConn.User) > 0 && len(irodsConn.Password) > 0 {
		mountSensitiveOptions = append(mountSensitiveOptions, fmt.Sprintf("username=%s", irodsConn.User))
		stdinArgs = append(stdinArgs, irodsConn.Password)
	}

	klog.V(5).Infof("Mounting %s (%s) at %s with options %v", source, fsType, targetPath, mountOptions)
	if err := driver.mounter.MountSensitive2(source, source, targetPath, fsType, mountOptions, mountSensitiveOptions, stdinArgs); err != nil {
		return status.Errorf(codes.Internal, "Could not mount %q (%q) at %q: %v", source, fsType, targetPath, err)
	}

	return nil
}

func (driver *Driver) mountNfs(volContext map[string]string, volSecrets map[string]string, mntOptions []string, targetPath string) error {
	irodsConn, err := ExtractIRODSNFSConnection(volContext, volSecrets)
	if err != nil {
		return err
	}

	fsType := "nfs"
	source := fmt.Sprintf("%s:%s", irodsConn.Hostname, irodsConn.Path)

	mountOptions := []string{}
	mountSensitiveOptions := []string{}
	stdinArgs := []string{}

	mountOptions = append(mountOptions, mntOptions...)

	if irodsConn.Port != 2049 {
		mountOptions = append(mountOptions, fmt.Sprintf("port=%d", irodsConn.Port))
	}

	klog.V(5).Infof("Mounting %s (%s) at %s with options %v", source, fsType, targetPath, mountOptions)
	if err := driver.mounter.MountSensitive2(source, source, targetPath, fsType, mountOptions, mountSensitiveOptions, stdinArgs); err != nil {
		return status.Errorf(codes.Internal, "Could not mount %q (%q) at %q: %v", source, fsType, targetPath, err)
	}

	return nil
}
