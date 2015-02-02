package nfs_mount

import (
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet/volume"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet/volume/mount_util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/types"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/mount"
	"github.com/golang/glog"
)

// This is the primary entrypoint for volume plugins.
func ProbeVolumePlugins() []volume.Plugin {
	return []volume.Plugin{&nfsMountPlugin{nil}}
}

type nfsMountPlugin struct {
	host volume.Host
}

var _ volume.Plugin = &nfsMountPlugin{}

const (
	nfsMountPluginName = "kubernetes.io/nfs-mount"
)

func (plugin *nfsMountPlugin) Init(host volume.Host) {
	plugin.host = host
}

func (plugin *nfsMountPlugin) Name() string {
	return nfsMountPluginName
}

func (plugin *nfsMountPlugin) CanSupport(spec *api.Volume) bool {
	if spec.Source.NFSMount != nil {
		return true
	}
	return false
}

func (plugin *nfsMountPlugin) NewBuilder(spec *api.Volume, podUID types.UID) (volume.Builder, error) {
	return plugin.newBuilderInternal(spec, podUID, mount.New())
}

func (plugin *nfsMountPlugin) newBuilderInternal(spec *api.Volume, podUID types.UID, mounter mount.Interface) (volume.Builder, error) {

	return &nfsMount{
		server:       spec.Source.NFSMount.Server,
		path:         spec.Source.NFSMount.SourcePath,
		mountOptions: spec.Source.NFSMount.MountOptions,
		podUID:       podUID,
		mounter:      mounter,
		plugin:       plugin,
	}, nil
}

func (plugin *nfsMountPlugin) NewCleaner(volName string, podUID types.UID) (volume.Cleaner, error) {
	return plugin.newCleanerInternal(volName, podUID, mount.New())
}

func (plugin *nfsMountPlugin) newCleanerInternal(volName string, podUID types.UID, mounter mount.Interface) (volume.Cleaner, error) {
	return &nfsMount{
		server:       "",
		path:         "",
		mountOptions: "",
		podUID:       podUID,
		mounter:      mounter,
		plugin:       plugin,
	}, nil
}

// NFSMount volumes represent a bare host file or directory mount of an NFS export.
// The direct at the specified path will be directly exposed to the container.
type nfsMount struct {
	podUID       types.UID
	server       string
	path         string
	mountOptions string
	mounter      mount.Interface
	plugin       *nfsMountPlugin
}

func (nfs *nfsMount) SetUp() error {
	path := strings.Split(nfs.GetPath(), ":")
	if len(path) != 2 {
		return fmt.Errorf("Mount path must be of format /export/path:/mount/path")
	}
	exportDir := path[0]
	mountDir := path[1]
	flags := uintptr(0)
	if strings.Contains(nfs.mountOptions, "ro") {
		flags = mount.FlagReadOnly
	}
	// NFS Mount format is server:/export/path /mount
	err := nfs.mounter.Mount(nfs.server+":"+exportDir, mountDir, "", mount.FlagBind|flags, "")
	if err != nil {
		mountpoint, mntErr := mount_util.IsMountPoint(mountDir)
		if mntErr != nil {
			glog.Errorf("isMountpoint check failed: %v", mntErr)
			return err
		}
		if mountpoint {
			if mntErr = nfs.mounter.Unmount(mountDir, 0); mntErr != nil {
				glog.Errorf("Failed to unmount: %v", mntErr)
				return err
			}
			mountpoint, mntErr := mount_util.IsMountPoint(mountDir)
			if mntErr != nil {
				glog.Errorf("isMountpoint check failed: %v", mntErr)
				return err
			}
			if mountpoint {
				// This is very odd, we don't expect it.  We'll try again next sync loop.
				glog.Errorf("%s is still mounted, despite call to unmount().  Will try again next sync loop.", mountDir)
				return err
			}
		}
		return err
	}

	return nil
}

func (nfs *nfsMount) GetPath() string {
	return nfs.path
}

func (nfs *nfsMount) GetServer() string {
	return nfs.server
}

func (nfs *nfsMount) GetMountOptions() string {
	return nfs.mountOptions
}

// TearDown does nothing.
func (nfs *nfsMount) TearDown() error {
	return nil
}
