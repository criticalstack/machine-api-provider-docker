package docker

import (
	"context"
	"encoding/base64"
	"fmt"

	cinderapi "github.com/criticalstack/crit/cmd/cinder/api"
	configutil "github.com/criticalstack/crit/pkg/config/util"
	critv1 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/criticalstack/crit/pkg/kubernetes/pki"
	"github.com/criticalstack/crit/pkg/log"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	infrav1 "github.com/criticalstack/machine-api-provider-docker/api/v1alpha1"
	machinev1 "github.com/criticalstack/machine-api/api/v1alpha1"
)

func CreateMachine(ctx context.Context, m *infrav1.DockerMachine, mcfg *machinev1.Config) error {
	obj, err := configutil.Unmarshal([]byte(mcfg.Spec.Config))
	if err != nil {
		return err
	}

	switch c := obj.(type) {
	case *critv1.ControlPlaneConfiguration:
		return errors.New("create control plane node not implemented")
	case *critv1.WorkerConfiguration:
		return CreateWorkerMachine(ctx, m, mcfg, c)
	default:
		return errors.Errorf("received invalid configuration type: %T", c)
	}
}

func CreateWorkerMachine(ctx context.Context, m *infrav1.DockerMachine, mcfg *machinev1.Config, wcfg *critv1.WorkerConfiguration) error {
	cfg := &cinderapi.ClusterConfiguration{
		PreCritCommands:     mcfg.Spec.PreCritCommands,
		PostCritCommands:    mcfg.Spec.PostCritCommands,
		WorkerConfiguration: wcfg,
	}
	for _, f := range mcfg.Spec.Files {
		cfg.Files = append(cfg.Files, cinderapi.File{
			Path:        f.Path,
			Owner:       f.Owner,
			Permissions: f.Permissions,
			Encoding:    cinderapi.Encoding(f.Encoding),
			Content:     f.Content,
		})
	}

	// NOTE(chrism): this is a bit hacky rn getting the join info and will
	// eventually change
	cp, err := getControlPlaneNode(m.Spec.ClusterName)
	if err != nil {
		return err
	}
	cfg.WorkerConfiguration.ControlPlaneEndpoint.Host = cp.IP()
	cfg.WorkerConfiguration.ControlPlaneEndpoint.Port = 6443
	data, err := cp.ReadFile("/etc/kubernetes/pki/ca.crt")
	if err != nil {
		return err
	}
	cfg.Files = append(cfg.Files, cinderapi.File{
		Path:        "/etc/kubernetes/pki/ca.crt",
		Owner:       "root:root",
		Permissions: "0644",
		Encoding:    cinderapi.Encoding("base64"),
		Content:     base64.StdEncoding.EncodeToString(data),
	})
	id, secret := pki.GenerateBootstrapToken()
	token := fmt.Sprintf("%s.%s", id, secret)
	if err := cp.Command("crit", "create", "token", token).Run(); err != nil {
		return err
	}
	cfg.WorkerConfiguration.BootstrapToken = token
	cfg.WorkerConfiguration.NodeConfiguration.KubernetesVersion = cinderapi.Version
	data, err = configutil.Marshal(cfg.WorkerConfiguration)
	if err != nil {
		return err
	}
	cfg.Files = append(cfg.Files, cinderapi.File{
		Path:        "/var/lib/crit/config.yaml",
		Owner:       "root:root",
		Permissions: "0644",
		Encoding:    cinderapi.Encoding("base64"),
		Content:     base64.StdEncoding.EncodeToString(data),
	})

	node, err := cinderapi.CreateWorkerNode(ctx, &cinderapi.WorkerConfig{
		ClusterName:          m.Spec.ClusterName,
		ContainerName:        m.Spec.ContainerName,
		Image:                m.Spec.Image,
		Verbose:              true,
		ClusterConfiguration: cfg,
	})
	if err != nil {
		return err
	}
	log.Info("worker node created", zap.Stringer("name", node))
	return nil
}

func DeleteMachine(ctx context.Context, m *infrav1.DockerMachine) error {
	nodes, err := cinderapi.ListNodes(m.Spec.ClusterName)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.String() == m.Spec.ContainerName {
			return cinderapi.DeleteNodes([]*cinderapi.Node{n})
		}
	}
	return errors.Errorf("cannot find machine %q to delete", m.Spec.ContainerName)
}

func MachineExists(ctx context.Context, m *infrav1.DockerMachine) (bool, error) {
	nodes, err := cinderapi.ListNodes(m.Spec.ClusterName)
	if err != nil {
		return false, err
	}

	for _, n := range nodes {
		if n.String() == m.Spec.ContainerName {
			return true, nil
		}
	}
	return false, nil
}

func getControlPlaneNode(clusterName string) (*cinderapi.Node, error) {
	nodes, err := cinderapi.ListNodes(clusterName)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.String() == clusterName {
			return node, nil
		}
	}
	return nil, errors.Errorf("cannot find cluster control-plane: %q", clusterName)
}
