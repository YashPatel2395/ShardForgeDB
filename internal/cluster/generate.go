package cluster

import (
	"fmt"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
	"github.com/YashPatel2395/ShardForgeDB/internal/proxy"
)

// NodeConfigs converts the cluster config's node list to gateway.NodeConfig slice.
// Returns ErrInvalidConfig if Validate fails.
func NodeConfigs(cfg Config) ([]gateway.NodeConfig, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	ncs := make([]gateway.NodeConfig, len(cfg.Nodes))
	for i, n := range cfg.Nodes {
		ncs[i] = gateway.NodeConfig{
			ID:      n.ID,
			BaseURL: n.BaseURL,
			Weight:  n.Weight,
		}
	}
	return ncs, nil
}

// GatewayOptions builds a gateway.Options from the cluster config and the given timeout.
// timeout == 0 uses the gateway default (5s).
// Returns an error if Validate fails.
func GatewayOptions(cfg Config, timeout time.Duration) (gateway.Options, error) {
	ncs, err := NodeConfigs(cfg)
	if err != nil {
		return gateway.Options{}, fmt.Errorf("cluster: gateway options: %w", err)
	}
	return gateway.Options{
		Nodes:        ncs,
		VirtualNodes: cfg.Routing.VirtualNodes,
		Timeout:      timeout,
	}, nil
}

// ProxyOptions builds a proxy.Options from the cluster config and the given timeout.
// Uses cfg.Proxy.Addr as the proxy listen address.
// timeout == 0 uses the proxy default (5s).
// Returns an error if Validate fails.
func ProxyOptions(cfg Config, timeout time.Duration) (proxy.Options, error) {
	ncs, err := NodeConfigs(cfg)
	if err != nil {
		return proxy.Options{}, fmt.Errorf("cluster: proxy options: %w", err)
	}
	addr := cfg.Proxy.Addr
	if addr == "" {
		addr = DefaultProxyAddr
	}
	return proxy.Options{
		Addr:         addr,
		Nodes:        ncs,
		VirtualNodes: cfg.Routing.VirtualNodes,
		Timeout:      timeout,
	}, nil
}
