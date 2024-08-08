package connector

import (
	"context"
	"fmt"
	"os"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/stretchr/testify/assert"
)

var (
	ctx          = context.Background()
	accountID    = os.Getenv("BATON_ACCOUNT_ID")
	clientID     = os.Getenv("BATON_ZOOM_CLIENT_ID")
	clientSecret = os.Getenv("BATON_ZOOM_CLIENT_SECRET")
)

func TestUserResourceTypeList(t *testing.T) {
	if clientID == "" && clientSecret == "" && accountID == "" {
		t.Skip()
	}

	cli, err := getClientForTesting(ctx)
	assert.Nil(t, err)

	user := &userResourceType{
		resourceType: &v2.ResourceType{},
		client:       cli.client,
	}
	rs, _, _, err := user.List(ctx, &v2.ResourceId{}, &pagination.Token{})
	assert.Nil(t, err)
	assert.NotNil(t, rs)
}

func getClientForTesting(ctx context.Context) (*Zoom, error) {
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	token, err := zoom.RequestAccessToken(ctx, accountID, clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("zoom-connector: failed to get token: %w", err)
	}

	return &Zoom{
		client: zoom.NewClient(httpClient, token),
	}, nil
}
