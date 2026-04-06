package main

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-sdk/pkg/cli"
	sdkconfig "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
	"github.com/conductorone/baton-zoom/pkg/config"
	"github.com/conductorone/baton-zoom/pkg/connector"
)

const (
	version       = "dev"
	connectorName = "baton-zoom"
)

func main() {
	ctx := context.Background()
	sdkconfig.RunConnector(ctx,
		connectorName,
		version,
		config.Config,
		getConnector,
		connectorrunner.WithDefaultCapabilitiesConnectorBuilderV2(connector.NewForCapabilities()),
	)
}

func getConnector(ctx context.Context, cfg *config.Zoom, _ *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	cb, err := connector.New(
		ctx,
		cfg.AccountId,
		cfg.ZoomClientId,
		cfg.ZoomClientSecret,
		cfg.SyncInactiveUsers,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: error creating connector: %w", err)
	}

	return cb, nil, nil
}
