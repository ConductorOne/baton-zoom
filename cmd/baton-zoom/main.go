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

func getConnector(ctx context.Context, cfg *config.Zoom, opts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	// License grants are emitted from the user syncer (Zoom has no /licenses
	// endpoint), so the user builder needs to know whether the customer's sync
	// filter includes the license resource type. WillSyncResourceType returns
	// true when licenses are explicitly selected or when no filter is set at
	// all (e.g. local CLI runs).
	syncLicenses := true
	if opts != nil {
		syncLicenses = opts.WillSyncResourceType(connector.LicenseResourceTypeID)
	}

	cb, err := connector.New(
		ctx,
		cfg.AccountId,
		cfg.ZoomClientId,
		cfg.ZoomClientSecret,
		cfg.SyncInactiveUsers,
		cfg.BaseUrl,
		syncLicenses,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: error creating connector: %w", err)
	}

	return cb, nil, nil
}
