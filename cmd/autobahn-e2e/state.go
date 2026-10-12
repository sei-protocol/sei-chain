package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const stateVersion = 1

var clusterNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

type clusterState struct {
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	RepoRoot  string    `json:"repo_root,omitempty"`
	Nodes     []node    `json:"nodes"`
	AWS       *awsState `json:"aws,omitempty"`
}

type node struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Container   string `json:"container"`
	EVMHostPort int    `json:"evm_host_port"`
}

const (
	awsRoleValidator = "validator"
	awsRoleLoad      = "load"
)

type awsHost struct {
	Role       string `json:"role"`
	Index      int    `json:"index,omitempty"`
	InstanceID string `json:"instance_id"`
	PublicIP   string `json:"public_ip"`
	PrivateIP  string `json:"private_ip,omitempty"`
}

type awsState struct {
	Region          string    `json:"region"`
	Profile         string    `json:"profile,omitempty"`
	Topology        string    `json:"topology,omitempty"`
	InstanceID      string    `json:"instance_id,omitempty"`
	PublicIP        string    `json:"public_ip,omitempty"`
	Hosts           []awsHost `json:"hosts,omitempty"`
	SecurityGroupID string    `json:"security_group_id,omitempty"`
	KeyName         string    `json:"key_name,omitempty"`
	SSHKeyPath      string    `json:"ssh_key_path,omitempty"`
	SSHUser         string    `json:"ssh_user"`
	RemoteDir       string    `json:"remote_dir"`
	ManagedKey      bool      `json:"managed_key"`
	RepoURL         string    `json:"repo_url"`
	Ref             string    `json:"ref"`
}

func (s *awsState) topology() string {
	if s == nil {
		return awsTopologyDistributed
	}
	switch s.Topology {
	case awsTopologyColocated, awsTopologyDistributed:
		return s.Topology
	}
	if len(s.validators()) >= 2 {
		return awsTopologyDistributed
	}
	return awsTopologyColocated
}

func (s *awsState) colocated() bool {
	return s.topology() == awsTopologyColocated
}

func (s *awsState) evmPort(n node) int {
	if s.colocated() {
		return n.EVMHostPort
	}
	return awsEVMPort
}

func (s *awsState) validators() []awsHost {
	if s == nil {
		return nil
	}
	hosts := make([]awsHost, 0, len(s.Hosts))
	for _, host := range s.Hosts {
		if host.Role == awsRoleValidator {
			hosts = append(hosts, host)
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Index < hosts[j].Index })
	return hosts
}

func (s *awsState) loadHost() (awsHost, bool) {
	if s == nil {
		return awsHost{}, false
	}
	for _, host := range s.Hosts {
		if host.Role == awsRoleLoad {
			return host, true
		}
	}
	if s.PublicIP != "" || s.InstanceID != "" {
		return awsHost{Role: awsRoleLoad, InstanceID: s.InstanceID, PublicIP: s.PublicIP}, true
	}
	return awsHost{}, false
}

func (s *awsState) validatorByIndex(index int) (awsHost, bool) {
	for _, host := range s.validators() {
		if host.Index == index {
			return host, true
		}
	}
	return awsHost{}, false
}

func (s *awsState) instanceIDs() []string {
	if s == nil {
		return nil
	}
	if len(s.Hosts) > 0 {
		ids := make([]string, 0, len(s.Hosts))
		seen := map[string]struct{}{}
		for _, host := range s.Hosts {
			if host.InstanceID == "" {
				continue
			}
			if _, ok := seen[host.InstanceID]; ok {
				continue
			}
			seen[host.InstanceID] = struct{}{}
			ids = append(ids, host.InstanceID)
		}
		return ids
	}
	if s.InstanceID != "" {
		return []string{s.InstanceID}
	}
	return nil
}

type stateStore struct {
	dir string
}

func newStateStore(dir string) stateStore {
	return stateStore{dir: dir}
}

func defaultStateDir() string {
	if value := os.Getenv("AUTOBAHN_E2E_STATE_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".autobahn-e2e"
	}
	return filepath.Join(home, ".sei", "autobahn-e2e")
}

func validateClusterName(name string) error {
	if !clusterNamePattern.MatchString(name) {
		return fmt.Errorf("cluster name %q must match %s", name, clusterNamePattern)
	}
	return nil
}

func (s stateStore) save(state clusterState) error {
	if err := validateClusterName(state.Name); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster state: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, state.Name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect temporary state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	if err := os.Rename(tmpName, s.path(state.Name)); err != nil {
		return fmt.Errorf("install cluster state: %w", err)
	}
	return nil
}

func (s stateStore) load(name string) (clusterState, error) {
	if err := validateClusterName(name); err != nil {
		return clusterState{}, err
	}
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		if os.IsNotExist(err) {
			return clusterState{}, fmt.Errorf("cluster %q does not exist", name)
		}
		return clusterState{}, fmt.Errorf("read cluster state: %w", err)
	}
	var state clusterState
	if err := json.Unmarshal(data, &state); err != nil {
		return clusterState{}, fmt.Errorf("decode cluster state: %w", err)
	}
	if state.Version != stateVersion {
		return clusterState{}, fmt.Errorf("cluster %q uses unsupported state version %d", name, state.Version)
	}
	return state, nil
}

func (s stateStore) loadAll() ([]clusterState, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state directory: %w", err)
	}
	states := make([]clusterState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		state, err := s.load(name)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, nil
}

func (s stateStore) exists(name string) (bool, error) {
	_, err := os.Stat(s.path(name))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect cluster state: %w", err)
}

func (s stateStore) remove(name string) error {
	if err := os.Remove(s.path(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cluster state: %w", err)
	}
	return nil
}

func (s stateStore) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}

func clusterNodes(count int) []node {
	nodes := make([]node, count)
	for i := range count {
		nodes[i] = node{
			Index:       i,
			Name:        fmt.Sprintf("node-%d", i),
			Container:   fmt.Sprintf("sei-node-%d", i),
			EVMHostPort: 8545 + 2*i,
		}
	}
	return nodes
}
