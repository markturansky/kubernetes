/*
Copyright 2014 Google Inc. All rights reserved.

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

package petco

import (
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/testapi"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/scheduler"
)

type fakeBinder struct {
	b func(binding *api.Binding) error
}

func (fb fakeBinder) Bind(binding *api.Binding) error { return fb.b(binding) }

func podWithID(id string) *api.Pod {
	return &api.Pod{ObjectMeta: api.ObjectMeta{Name: id, SelfLink: testapi.SelfLink("pods", id)}}
}

type mockScheduler struct {
	machine string
	err     error
}

func (es mockScheduler) Schedule(pod api.Pod, ml scheduler.MinionLister) (string, error) {
	return es.machine, es.err
}

func TestVolumeController(t *testing.T) {

}
