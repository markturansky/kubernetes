package nfs_mount

import (
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet/volume"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/types"
)

func TestCanSupport(t *testing.T) {
	plugMgr := volume.PluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), &volume.FakeHost{"fake"})

	plug, err := plugMgr.FindPluginByName("kubernetes.io/nfs-mount")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	if plug.Name() != "kubernetes.io/nfs-mount" {
		t.Errorf("Wrong name: %s", plug.Name())
	}
	if !plug.CanSupport(&api.Volume{Source: api.VolumeSource{NFSMount: &api.NFSMount{}}}) {
		t.Errorf("Expected true")
	}
	if plug.CanSupport(&api.Volume{Source: api.VolumeSource{}}) {
		t.Errorf("Expected false")
	}
}

func TestPlugin(t *testing.T) {
	plugMgr := volume.PluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), &volume.FakeHost{"fake"})

	plug, err := plugMgr.FindPluginByName("kubernetes.io/nfs-mount")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	spec := &api.Volume{
		Name:   "vol1",
		Source: api.VolumeSource{NFSMount: &api.NFSMount{"localhost", "/vol1", ""}},
	}
	builder, err := plug.NewBuilder(spec, types.UID("poduid"))
	if err != nil {
		t.Errorf("Failed to make a new Builder: %v", err)
	}
	if builder == nil {
		t.Errorf("Got a nil Builder: %v")
	}

	path := builder.GetPath()
	if path != "/vol1" {
		t.Errorf("Got unexpected path: %s", path)
	}

	if err := builder.SetUp(); err != nil {
		t.Errorf("Expected success, got: %v", err)
	}

	cleaner, err := plug.NewCleaner("vol1", types.UID("poduid"))
	if err != nil {
		t.Errorf("Failed to make a new Cleaner: %v", err)
	}
	if cleaner == nil {
		t.Errorf("Got a nil Cleaner: %v")
	}

	if err := cleaner.TearDown(); err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}
