package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	config "github.com/conductorone/baton-sdk/pb/c1/config/v1"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/actions"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	actionTransferAndDeleteUser = "transfer_and_delete_user"

	argUserID            = "user_id"
	argDeleteAction      = "action"
	argTransferEmail     = "transfer_email"
	argTransferMeeting   = "transfer_meeting"
	argTransferWebinar   = "transfer_webinar"
	argTransferRecording = "transfer_recording"
)

var transferAndDeleteUserSchema = &v2.BatonActionSchema{
	Name:        actionTransferAndDeleteUser,
	DisplayName: "Transfer Data and Delete User",
	Description: "Reassigns a user's meetings, webinars, and cloud recordings to another Zoom user, then removes the user from the account. This operation cannot be recovered.",
	Arguments: []*config.Field{
		{
			Name:        argUserID,
			DisplayName: "User",
			Description: "The Zoom user to remove.",
			IsRequired:  true,
			Field: &config.Field_ResourceIdField{
				ResourceIdField: &config.ResourceIdField{
					Rules: &config.ResourceIDRules{
						AllowedResourceTypeIds: []string{resourceTypeUser.Id},
					},
				},
			},
		},
		{
			Name:        argDeleteAction,
			DisplayName: "Action",
			Description: "Whether to disassociate the user from the account or permanently delete them.",
			IsRequired:  true,
			Field: &config.Field_StringField{
				StringField: &config.StringField{
					Rules: &config.StringRules{
						In: []string{string(zoom.Disassociate), string(zoom.Delete)},
					},
					Options: []*config.StringFieldOption{
						{Name: string(zoom.Disassociate), Value: string(zoom.Disassociate), DisplayName: "Disassociate"},
						{Name: string(zoom.Delete), Value: string(zoom.Delete), DisplayName: "Delete"},
					},
				},
			},
		},
		{
			Name:        argTransferEmail,
			DisplayName: "Transfer To",
			Description: "Email of the Zoom user to receive the transferred meetings, webinars, and recordings. Required if any transfer option below is enabled.",
			Field:       &config.Field_StringField{},
		},
		{
			Name:        argTransferMeeting,
			DisplayName: "Transfer Meetings",
			Description: "Transfer the user's scheduled meetings to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
		{
			Name:        argTransferWebinar,
			DisplayName: "Transfer Webinars",
			Description: "Transfer the user's scheduled webinars to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
		{
			Name:        argTransferRecording,
			DisplayName: "Transfer Cloud Recordings",
			Description: "Transfer the user's cloud recordings to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
	},
	ReturnTypes: []*config.Field{
		{Name: "success", DisplayName: "Success", Field: &config.Field_BoolField{}},
		{Name: "message", DisplayName: "Message", Field: &config.Field_StringField{}},
	},
	ActionType: []v2.ActionType{v2.ActionType_ACTION_TYPE_RESOURCE_DELETE},
}

var _ connectorbuilder.ResourceActionProvider = (*userResourceType)(nil)

func (u *userResourceType) ResourceActions(ctx context.Context, registry actions.ActionRegistry) error {
	if err := registry.Register(ctx, transferAndDeleteUserSchema, u.transferAndDeleteUserAction); err != nil {
		return fmt.Errorf("baton-zoom: register transfer_and_delete_user action: %w", err)
	}
	return nil
}

func (u *userResourceType) transferAndDeleteUserAction(
	ctx context.Context,
	args *structpb.Struct,
) (*structpb.Struct, annotations.Annotations, error) {
	userRef, err := actions.RequireResourceIDArg(args, argUserID)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	userID := userRef.GetResource()
	if userID == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id is required")
	}
	// RequireResourceIDArg doesn't enforce AllowedResourceTypeIds itself —
	// the platform does that before invocation, but --invoke-action (local
	// and CI testing) bypasses that check, so a wrong-type reference would
	// otherwise reach the delete call below.
	if resourceType := userRef.GetResourceType(); resourceType != "" && resourceType != resourceTypeUser.Id {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id must reference a %q resource, got %q", resourceTypeUser.Id, resourceType)
	}
	// A "/" is never valid in a Zoom user ID; reject it here rather than
	// relying solely on the client's URL escaping to keep it confined to
	// one path segment.
	if strings.Contains(userID, "/") {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id must not contain \"/\"")
	}

	deleteAction, err := actions.RequireStringArg(args, argDeleteAction)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	if deleteAction != string(zoom.Disassociate) && deleteAction != string(zoom.Delete) {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: action must be %q or %q", zoom.Disassociate, zoom.Delete)
	}

	transferEmail, _ := actions.GetStringArg(args, argTransferEmail)
	transferMeeting, _ := actions.GetBoolArg(args, argTransferMeeting)
	transferWebinar, _ := actions.GetBoolArg(args, argTransferWebinar)
	transferRecording, _ := actions.GetBoolArg(args, argTransferRecording)
	transferring := transferMeeting || transferWebinar || transferRecording

	if transferring && transferEmail == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer_email is required when transfer_meeting, transfer_webinar, or transfer_recording is set")
	}
	if strings.Contains(transferEmail, "/") {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer_email must not contain \"/\"")
	}

	// Confirm transfer_email resolves to a real Zoom user before the
	// destructive delete call. Zoom's DELETE /v2/users/{userId} returns the
	// same 404 whether userID or transfer_email is the one that doesn't
	// exist, with no structured field to tell them apart — so the only
	// reliable way to make a later 404 from that call unambiguous is to rule
	// out transfer_email as the cause beforehand.
	if transferEmail != "" {
		_, resp, err := u.client.GetUser(ctx, transferEmail)
		if err != nil {
			if isUserNotFound(err) {
				return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer_email user not found: %s", transferEmail)
			}
			return nil, nil, mapAPIError(err, fmt.Sprintf("baton-zoom: transfer_and_delete_user: verify transfer_email %s", transferEmail))
		}
		resp.Body.Close()
	}

	err = u.client.DeleteUserWithTransfer(ctx, userID, zoom.DeleteUserOptions{
		Action:            zoom.DeleteAction(deleteAction),
		TransferEmail:     transferEmail,
		TransferMeeting:   transferMeeting,
		TransferWebinar:   transferWebinar,
		TransferRecording: transferRecording,
	})
	if err != nil {
		if isUserNotFound(err) {
			if !transferring {
				return actions.NewReturnValues(
					true,
					actions.NewStringReturnField("message", fmt.Sprintf("user %s was already removed from the account", userID)),
				), nil, nil
			}
			// The recipient preflight above only proves transfer_email existed
			// before this call — it's no proof the transfer itself completed.
			// A 404 here could mean the delete-with-transfer already ran
			// (safe to treat as done) or that it never ran with a transfer at
			// all (e.g. a prior no-transfer delete already removed the user).
			// Zoom gives no signal to tell those apart, so don't claim success.
			return nil, nil, status.Errorf(codes.FailedPrecondition,
				"baton-zoom: transfer_and_delete_user: user %s was already removed from the account, but the requested transfer to %s cannot be confirmed; verify manually", userID, transferEmail)
		}
		return nil, nil, mapAPIError(err, fmt.Sprintf("baton-zoom: transfer_and_delete_user: %s", userID))
	}

	message := fmt.Sprintf("user %s %sd from the account", userID, deleteAction)
	if transferring {
		message = fmt.Sprintf("user %s data transferred and %sd from the account", userID, deleteAction)
	}
	return actions.NewReturnValues(true, actions.NewStringReturnField("message", message)), nil, nil
}

// isUserNotFound reports whether err is a Zoom 404, meaning the target is
// already gone — re-invoking transfer_and_delete_user on an already-deleted
// user must succeed, not fail.
func isUserNotFound(err error) bool {
	var apiErr *zoom.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// mapAPIError maps a *zoom.APIError's HTTP status onto the gRPC code the
// platform uses to decide retry vs. permanent failure. pkg/zoom uses a raw
// http.Client rather than uhttp, so no error from it otherwise carries a
// status code. Delegates to uhttp.GrpcCodeFromHTTPStatus — the same mapping
// the SDK's own retry classifier keys off of (only codes.Unavailable and
// codes.DeadlineExceeded are retried) — rather than hand-rolling a parallel
// mapping that can drift from it. uhttp.WrapErrors joins the resulting
// status with the original error so errors.As(*zoom.APIError) still works.
// Errors that aren't a *zoom.APIError are wrapped as-is.
func mapAPIError(err error, prefix string) error {
	var apiErr *zoom.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	code := uhttp.GrpcCodeFromHTTPStatus(apiErr.StatusCode)
	return uhttp.WrapErrors(code, fmt.Sprintf("%s: %v", prefix, err), err)
}
