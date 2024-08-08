package connector

import (
	"context"
	"os"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

var (
	ctx          = context.Background()
	accountID    = os.Getenv("BATON_ACCOUNT_ID")
	clientID     = os.Getenv("BATON_ZOOM_CLIENT_ID")
	clientSecret = os.Getenv("BATON_ZOOM_CLIENT_SECRET")
)

// This test assumes there is a local.env file.
func TestUserResourceTypeList(t *testing.T) {
	if clientID == "" && clientSecret == "" && accountID == "" {
		t.Skip()
	}

	viper.AddConfigPath("../../")
	viper.SetConfigName("local")
	viper.SetConfigType("env")
	viper.AutomaticEnv()
	err := viper.ReadInConfig()
	if err != nil {
		t.Fail()
	}

	cli, err := getClientForTesting(ctx, viper.GetViper())
	assert.Nil(t, err)

	user := &userResourceType{
		resourceType: &v2.ResourceType{},
		client:       cli,
	}
	rs, _, _, err := user.List(ctx, &v2.ResourceId{}, &pagination.Token{})
	assert.Nil(t, err)
	assert.NotNil(t, rs)
}

func getClientForTesting(ctx context.Context, cfg *viper.Viper) (*zoom.Client, error) {
	cli, err := New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return cli.client, nil
}
