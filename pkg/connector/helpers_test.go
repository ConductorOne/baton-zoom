package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupEntitlementSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		want    string
		wantErr bool
	}{
		{
			name: "member",
			id:   "group:group-id:member",
			want: memberEntitlement,
		},
		{
			name: "admin",
			id:   "group:group-id:admin",
			want: adminEntitlement,
		},
		{
			name:    "unknown slug",
			id:      "group:group-id:assigned",
			wantErr: true,
		},
		{
			name:    "too short",
			id:      "group:member",
			wantErr: true,
		},
		{
			name:    "empty",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := groupEntitlementSlug(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
