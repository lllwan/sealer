// Copyright © 2021 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"fmt"
	"sync"

	"github.com/alibaba/sealer/logger"
)

func (k *KubeadmRuntime) reset() error {
	k.resetNodes(k.getNodesIPList())
	k.resetMasters(k.getMasterIPList())
	return k.DeleteRegistry()
}

func (k *KubeadmRuntime) resetNodes(nodes []string) {
	var wg sync.WaitGroup
	for _, node := range nodes {
		wg.Add(1)
		go func(node string) {
			defer wg.Done()
			if err := k.resetNode(node); err != nil {
				logger.Error("delete node %s failed %v", node, err)
			}
		}(node)
	}
	wg.Wait()
}

func (k *KubeadmRuntime) resetMasters(nodes []string) {
	for _, node := range nodes {
		if err := k.resetNode(node); err != nil {
			logger.Error("delete master %s failed %v", node, err)
		}
	}
}

func (k *KubeadmRuntime) resetNode(node string) error {
	ssh, err := k.getHostSSHClient(node)
	if err != nil {
		return fmt.Errorf("reset node failed %v", err)
	}
	if err := ssh.CmdAsync(node, fmt.Sprintf(RemoteCleanMasterOrNode, vlogToStr(k.Vlog)),
		fmt.Sprintf(RemoteRemoveAPIServerEtcHost, k.getAPIServerDomain()),
		fmt.Sprintf(RemoteRemoveAPIServerEtcHost, getRegistryHost(k.getRootfs(), k.getMaster0IP()))); err != nil {
		return err
	}
	return nil
}
