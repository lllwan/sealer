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
	"path/filepath"

	"golang.org/x/crypto/bcrypt"

	"github.com/alibaba/sealer/logger"
	"github.com/alibaba/sealer/utils"
	"github.com/alibaba/sealer/utils/mount"
)

const (
	RegistryName                = "sealer-registry"
	RegistryBindDest            = "/var/lib/registry"
	RegistryMountUpper          = "/var/lib/sealer/tmp/upper"
	RegistryMountWork           = "/var/lib/sealer/tmp/work"
	SeaHub                      = "sea.hub"
	DefaultRegistryHtPasswdFile = "registry_htpasswd"
	DockerLoginCommand          = "docker login %s -u %s -p %s"
)

type RegistryConfig struct {
	IP       string `yaml:"ip,omitempty"`
	Domain   string `yaml:"domain,omitempty"`
	Port     string `yaml:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func getRegistryHost(rootfs, defaultRegistry string) (host string) {
	cf := GetRegistryConfig(rootfs, defaultRegistry)
	ip, _ := utils.GetSSHHostIPAndPort(cf.IP)
	return fmt.Sprintf("%s %s", ip, cf.Domain)
}

// ApplyRegistry Only use this for join and init, due to the initiation operations.
func (k *KubeadmRuntime) ApplyRegistry() error {
	cf := GetRegistryConfig(k.getRootfs(), k.getMaster0IP())
	ssh, err := k.getHostSSHClient(cf.IP)
	if err != nil {
		return fmt.Errorf("failed to get registry ssh client: %v", err)
	}

	mkdir := fmt.Sprintf("rm -rf %s %s && mkdir -p %s %s", RegistryMountUpper, RegistryMountWork,
		RegistryMountUpper, RegistryMountWork)

	mountCmd := fmt.Sprintf("%s && mount -t overlay overlay -o lowerdir=%s,upperdir=%s,workdir=%s %s", mkdir,
		k.getRootfs(),
		RegistryMountUpper, RegistryMountWork, k.getRootfs())
	isMount, _ := mount.GetRemoteMountDetails(ssh, cf.IP, k.getRootfs())
	if isMount {
		mountCmd = fmt.Sprintf("umount %s && %s", k.getRootfs(), mountCmd)
	}
	if err := ssh.CmdAsync(cf.IP, mountCmd); err != nil {
		return err
	}
	if cf.Username != "" && cf.Password != "" {
		htpasswd, err := cf.GenerateHtPasswd()
		if err != nil {
			return err
		}
		err = ssh.CmdAsync(cf.IP, fmt.Sprintf("echo '%s' >> %s", htpasswd, filepath.Join(k.getRootfs(), "etc", DefaultRegistryHtPasswdFile)))
		if err != nil {
			return err
		}
	}
	initRegistry := fmt.Sprintf("cd %s/scripts && sh init-registry.sh %s %s", k.getRootfs(), cf.Port, fmt.Sprintf("%s/registry", k.getRootfs()))
	addRegistryHosts := fmt.Sprintf(RemoteAddEtcHosts, getRegistryHost(k.getRootfs(), k.getMaster0IP()))
	if err = ssh.CmdAsync(cf.IP, initRegistry); err != nil {
		return err
	}
	if err = ssh.CmdAsync(k.getMaster0IP(), addRegistryHosts); err != nil {
		return err
	}
	if cf.Username == "" || cf.Password == "" {
		return nil
	}
	return ssh.CmdAsync(k.getMaster0IP(), fmt.Sprintf(DockerLoginCommand, cf.Domain+":"+cf.Port, cf.Username, cf.Password))
}

func (r *RegistryConfig) GenerateHtPasswd() (string, error) {
	if r.Username == "" || r.Password == "" {
		return "", fmt.Errorf("generate htpasswd failed: registry username or passwodr is empty")
	}
	pwdHash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to generate registry password: %v", err)
	}
	return r.Username + ":" + string(pwdHash), nil
}

func GetRegistryConfig(rootfs, defaultRegistry string) *RegistryConfig {
	var config RegistryConfig
	var DefaultConfig = &RegistryConfig{
		IP:     defaultRegistry,
		Domain: SeaHub,
		Port:   "5000",
	}
	registryConfigPath := filepath.Join(rootfs, "etc", "registry.yml")
	if !utils.IsFileExist(registryConfigPath) {
		logger.Debug("use default registry config")
		return DefaultConfig
	}
	err := utils.UnmarshalYamlFile(registryConfigPath, &config)
	if err != nil {
		logger.Error("Failed to read registry config! ")
		return DefaultConfig
	}
	if config.IP == "" {
		config.IP = DefaultConfig.IP
	} else {
		ip, port := utils.GetSSHHostIPAndPort(config.IP)
		config.IP = fmt.Sprintf("%s:%s", ip, port)
	}
	if config.Port == "" {
		config.Port = DefaultConfig.Port
	}
	if config.Domain == "" {
		config.Domain = DefaultConfig.Domain
	}
	logger.Debug(fmt.Sprintf("show registry info, IP: %s, Domain: %s", config.IP, config.Domain))
	return &config
}

func (k *KubeadmRuntime) DeleteRegistry() error {
	cf := GetRegistryConfig(k.getRootfs(), k.getMaster0IP())
	delDir := fmt.Sprintf("rm -rf %s %s", RegistryMountUpper, RegistryMountWork)
	ssh, err := k.getHostSSHClient(cf.IP)
	if err != nil {
		return fmt.Errorf("failed to delete registry: %v", err)
	}

	isMount, _ := mount.GetRemoteMountDetails(ssh, cf.IP, k.getRootfs())
	if isMount {
		delDir = fmt.Sprintf("umount %s && %s", k.getRootfs(), delDir)
	}
	cmd := fmt.Sprintf("if docker inspect %s;then docker rm -f %s;fi && %s ", RegistryName, RegistryName, delDir)
	return ssh.CmdAsync(cf.IP, cmd)
}
