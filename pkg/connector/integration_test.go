package connector

import (
	"context"
	"fmt"
	"os"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	rs, _, err := user.List(ctx, &v2.ResourceId{}, resource.SyncOpAttrs{})
	assert.Nil(t, err)
	assert.NotNil(t, rs)
}

func TestLicenseGrantRejectsNonUserPrincipal(t *testing.T) {
	principal := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeGroup.Id,
			Resource:     "group-id",
		}.Build(),
	}.Build()

	_, _, err := (&licenseResourceType{}).Grant(t.Context(), principal, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func getClientForTesting(ctx context.Context) (*Zoom, error) {
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	token, err := zoom.RequestAccessToken(ctx, accountID, clientID, clientSecret, "")
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to get token: %w", err)
	}

	zoomClient, err := zoom.NewClient(ctx, httpClient, token, "")
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to create client: %w", err)
	}

	return &Zoom{client: zoomClient}, nil
}
