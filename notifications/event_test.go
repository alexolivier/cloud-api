// Copyright 2021-2026 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package notifications_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	enginev1 "github.com/cerbos/cerbos/api/genpb/cerbos/engine/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessrequestv1 "github.com/cerbos/cloud-api/genpb/cerbos/cloud/accessrequest/v1"
	notificationsv1 "github.com/cerbos/cloud-api/genpb/cerbos/cloud/notifications/v1"
)

const (
	workspaceID  = "B6C0NNZO5VO6"
	deploymentID = "D4F8H2J6L0N3"
	buildID      = "X9K2M4P7Q1R3S5T8"
	channelID    = "C7H1K5M9P3R6"
	clientID     = "K2L4N6P8R0T2"
	memberID     = "M1N3P5R7T9V1"
	requestID    = "R1S2T3U4V5W6"
	approvalID   = "A1B2C3D4E5F6"
	evaluationID = "01J9Z7QK3M5N8P2R4T6V8X0Y1A"
	// Stands in for an approval JWT; only its presence matters to the rules.
	placeholderJWT = "header.payload.signature"
)

var memberActor = &notificationsv1.Actor{
	Actor: &notificationsv1.Actor_MemberId{MemberId: memberID},
}

// payloads builds a minimal valid payload for every event type. It is the
// fixture set for the whole-contract tests below, so a new EventType without an
// entry here fails TestEveryEventTypeValidates.
var payloads = map[notificationsv1.EventType]func(*notificationsv1.Event){
	notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_BuildFailed{
			BuildFailed: &notificationsv1.BuildFailed{
				Build: build(),
				Failure: &notificationsv1.BuildFailed_Compilation_{
					Compilation: &notificationsv1.BuildFailed_Compilation{
						Errors: []*notificationsv1.BuildFailed_Compilation_Error{{
							Kind:  notificationsv1.BuildFailed_Compilation_Error_KIND_COMPILE_ERROR,
							File:  "resource_policies/leave_request.yaml",
							Error: "undefined variable 'reqest'",
							Position: &notificationsv1.BuildFailed_Compilation_Error_Position{
								Line:   12,
								Column: 9,
								Path:   "$.resourcePolicy.rules[0].condition",
							},
						}},
						ErrorCount: 1,
					},
				},
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_BUILD_SUCCEEDED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_BuildSucceeded{
			BuildSucceeded: &notificationsv1.BuildSucceeded{
				Build: build(),
				Tests: &notificationsv1.TestTally{TestsCount: 3, PassedCount: 3},
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_BUNDLE_PROMOTED: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_BundlePromoted{
			BundlePromoted: &notificationsv1.BundlePromoted{BuildId: buildID},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CREDENTIAL_CREATED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_CredentialCreated{
			CredentialCreated: &notificationsv1.CredentialCreated{
				ClientId:    clientID,
				Description: "CI deploy",
				Actor:       memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CREDENTIAL_DELETED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_CredentialDeleted{
			CredentialDeleted: &notificationsv1.CredentialDeleted{ClientId: clientID, Actor: memberActor},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_MEMBER_ADDED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_MemberAdded{
			MemberAdded: &notificationsv1.MemberAdded{
				Member: &notificationsv1.Member{Id: memberID, Email: new("alice@example.com")},
				Roles:  []notificationsv1.WorkspaceRole{notificationsv1.WorkspaceRole_WORKSPACE_ROLE_DEVELOPER},
				Actor:  memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_MEMBER_REMOVED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_MemberRemoved{
			MemberRemoved: &notificationsv1.MemberRemoved{
				Member: &notificationsv1.Member{Id: memberID},
				Roles:  []notificationsv1.WorkspaceRole{notificationsv1.WorkspaceRole_WORKSPACE_ROLE_VIEWER},
				Actor:  memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CHANNEL_DISABLED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_ChannelDisabled{
			ChannelDisabled: &notificationsv1.ChannelDisabled{
				ChannelId: channelID,
				Reason:    notificationsv1.ChannelDisabled_REASON_DELIVERY_FAILURES,
				Details:   "503 Service Unavailable",
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_TEST_FIRED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_TestFired{
			TestFired: &notificationsv1.TestFired{ChannelId: channelID, Actor: memberActor},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_FLEET_DARK: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_DeploymentFleetDark{
			DeploymentFleetDark: &notificationsv1.DeploymentFleetDark{Condition: condition()},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_BUNDLE_LAGGING: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_DeploymentBundleLagging{
			DeploymentBundleLagging: &notificationsv1.DeploymentBundleLagging{Condition: condition()},
		}
	},
	// Access request events are workspace-level: none of these sets
	// DeploymentId, so the fixtures also prove that event.deployment_scoped
	// does not demand one.
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_PENDING: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestPending{
			AccessRequestPending: &notificationsv1.AccessRequestPending{
				AccessRequest: accessRequest(accessrequestv1.Status_STATUS_PENDING),
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_APPROVED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestApproved{
			AccessRequestApproved: &notificationsv1.AccessRequestApproved{
				AccessRequest: accessRequest(accessrequestv1.Status_STATUS_APPROVED),
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_DENIED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestDenied{
			AccessRequestDenied: &notificationsv1.AccessRequestDenied{
				AccessRequest: accessRequest(accessrequestv1.Status_STATUS_DENIED),
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_EXPIRED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestExpired{
			AccessRequestExpired: &notificationsv1.AccessRequestExpired{
				AccessRequest: accessRequest(accessrequestv1.Status_STATUS_EXPIRED),
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_CANCELLED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestCancelled{
			AccessRequestCancelled: &notificationsv1.AccessRequestCancelled{
				AccessRequest: accessRequest(accessrequestv1.Status_STATUS_CANCELLED),
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_SIGNING_KEY_EXPIRING: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_AccessRequestSigningKeyExpiring{
			AccessRequestSigningKeyExpiring: signingKeyExpiring(),
		}
	},
}

// signingKeyExpiring is a key published 91 days ago whose 90-day lifetime
// ended yesterday.
func signingKeyExpiring() *notificationsv1.SigningKeyExpiring {
	published := time.Now().Add(-91 * 24 * time.Hour)
	return &notificationsv1.SigningKeyExpiring{
		Kid:                  workspaceID + "-2",
		KeyVersion:           2,
		PublishedAt:          timestamppb.New(published),
		IntendedRetirementAt: timestamppb.New(published.Add(90 * 24 * time.Hour)),
	}
}

// accessRequest is a minimal valid request in the given status, as an event
// carries it: an approval without a token when approved, none otherwise.
func accessRequest(status accessrequestv1.Status) *accessrequestv1.AccessRequest {
	now := timestamppb.Now()
	ar := &accessrequestv1.AccessRequest{
		Id:     requestID,
		Status: status,
		Denial: &accessrequestv1.DeniedEvaluation{
			EvaluationId: evaluationID,
			EvaluatedAt:  now,
			Principal:    &enginev1.Principal{Id: "alice", Roles: []string{"engineer"}},
			Resource:     &enginev1.Resource{Kind: "production_database", Id: "orders"},
			Actions:      []string{"write"},
			Marker: &accessrequestv1.AccessRequestMarker{
				Approver:    "dba-oncall",
				ApprovalTtl: durationpb.New(4 * time.Hour),
				Reason:      "writes to production need a DBA",
			},
			RuleSrc: "resource.production_database.vdefault#write-requestable",
		},
		Verification:  accessrequestv1.Verification_VERIFICATION_MATCHED,
		Justification: "hotfix for INC-1234",
		CreatedAt:     now,
		ExpiresAt:     timestamppb.New(now.AsTime().Add(time.Hour)),
	}
	if status == accessrequestv1.Status_STATUS_APPROVED {
		ar.Approval = &accessrequestv1.Approval{
			Id:            approvalID,
			ApprovedAt:    now,
			ApprovedUntil: timestamppb.New(now.AsTime().Add(4 * time.Hour)),
		}
	}
	return ar
}

func build() *notificationsv1.Build {
	return &notificationsv1.Build{
		Id:            buildID,
		StoreIds:      []string{"S1T2U3V4W5X6"},
		DeploymentIds: []string{deploymentID},
	}
}

func condition() *notificationsv1.Condition {
	return &notificationsv1.Condition{
		State: notificationsv1.ConditionState_CONDITION_STATE_ENTERED,
		Since: timestamppb.Now(),
	}
}

func event(t *testing.T, eventType notificationsv1.EventType) *notificationsv1.Event {
	t.Helper()

	payload, ok := payloads[eventType]
	require.True(t, ok, "no fixture for %s", eventType)

	e := &notificationsv1.Event{
		Id:          "E1E2E3E4E5E6",
		Type:        eventType,
		OccurredAt:  timestamppb.Now(),
		WorkspaceId: workspaceID,
		Link:        "https://hub.cerbos.cloud/w/" + workspaceID,
	}
	payload(e)
	return e
}

// The webhook body documented in notifications.proto is protojson of the Event:
// the type as its enum name and the payload under a key named after the type.
func TestEventWebhookJSON(t *testing.T) {
	e := event(t, notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED)
	require.NoError(t, protovalidate.Validate(e), "fixture must be a valid event")

	body, err := protojson.Marshal(e)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "EVENT_TYPE_BUILD_FAILED", decoded["type"])
	require.Contains(t, decoded, "buildFailed")

	var roundTripped notificationsv1.Event
	require.NoError(t, protojson.Unmarshal(body, &roundTripped))
	require.Empty(t, cmp.Diff(e, &roundTripped, protocmp.Transform()))
}

// Every declared event type has a minimal valid event, so a transposed number
// in the type/payload CEL rule or a wrong entry in the deployment-scoped list
// shows up here.
func TestEveryEventTypeValidates(t *testing.T) {
	for _, eventType := range declaredEventTypes() {
		t.Run(eventType.String(), func(t *testing.T) {
			require.NoError(t, protovalidate.Validate(event(t, eventType)))
		})
	}
}

// The type/payload rule is a hand-written list of `this.type == N` clauses;
// adding an EventType without extending it would let the new type pass with
// the wrong payload.
func TestTypeMatchesPayloadRuleCoversEveryType(t *testing.T) {
	opts := (&notificationsv1.Event{}).ProtoReflect().Descriptor().Options()
	rules, ok := proto.GetExtension(opts, validate.E_Message).(*validate.MessageRules)
	require.True(t, ok, "Event must carry buf.validate.message rules")

	var expression string
	for _, rule := range rules.GetCel() {
		if rule.GetId() == "event.type_matches_payload" {
			expression = rule.GetExpression()
		}
	}
	require.NotEmpty(t, expression, "rule event.type_matches_payload not found")

	for _, eventType := range declaredEventTypes() {
		require.True(t, strings.Contains(expression, fmt.Sprintf("this.type == %d)", eventType)),
			"%s (%d) is missing from event.type_matches_payload", eventType, eventType)
	}
}

func TestEventValidation(t *testing.T) {
	testCases := []struct {
		name      string
		eventType notificationsv1.EventType
		mutate    func(*notificationsv1.Event)
		wantRule  string
	}{
		{
			name:      "workspace id of the wrong shape",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate:    func(e *notificationsv1.Event) { e.WorkspaceId = "b6c0nnzo5vo6" },
			wantRule:  "string.pattern",
		},
		{
			name:      "type names a different payload",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate:    func(e *notificationsv1.Event) { e.Type = notificationsv1.EventType_EVENT_TYPE_BUILD_SUCCEEDED },
			wantRule:  "event.type_matches_payload",
		},
		{
			name:      "deployment-scoped event without a deployment",
			eventType: notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_FLEET_DARK,
			mutate:    func(e *notificationsv1.Event) { e.DeploymentId = nil },
			wantRule:  "event.deployment_scoped",
		},
		{
			name:      "approved event carrying the approval token",
			eventType: notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_APPROVED,
			mutate: func(e *notificationsv1.Event) {
				e.GetAccessRequestApproved().GetAccessRequest().GetApproval().Token = placeholderJWT
			},
			wantRule: "access_request_approved.no_token",
		},
		{
			name:      "access request event whose request is in another status",
			eventType: notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_PENDING,
			mutate: func(e *notificationsv1.Event) {
				e.GetAccessRequestPending().GetAccessRequest().Status = accessrequestv1.Status_STATUS_DENIED
			},
			wantRule: "access_request_pending.status",
		},
		{
			name:      "signing key whose kid names another version",
			eventType: notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_SIGNING_KEY_EXPIRING,
			mutate:    func(e *notificationsv1.Event) { e.GetAccessRequestSigningKeyExpiring().KeyVersion = 3 },
			wantRule:  "signing_key_expiring.kid_names_version",
		},
		{
			name:      "signing key retiring before it was published",
			eventType: notificationsv1.EventType_EVENT_TYPE_ACCESS_REQUEST_SIGNING_KEY_EXPIRING,
			mutate: func(e *notificationsv1.Event) {
				k := e.GetAccessRequestSigningKeyExpiring()
				k.IntendedRetirementAt = timestamppb.New(k.GetPublishedAt().AsTime().Add(-time.Hour))
			},
			wantRule: "signing_key_expiring.retirement_after_publication",
		},
		{
			name:      "test failure in which nothing failed",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate: func(e *notificationsv1.Event) {
				e.GetBuildFailed().Failure = &notificationsv1.BuildFailed_Tests{
					Tests: &notificationsv1.TestTally{TestsCount: 3, PassedCount: 3},
				}
			},
			wantRule: "build_failed.tests.some_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := event(t, tc.eventType)
			tc.mutate(e)

			err := protovalidate.Validate(e)
			require.Error(t, err)

			var validationErr *protovalidate.ValidationError
			require.ErrorAs(t, err, &validationErr)
			require.Contains(t, ruleIDs(validationErr), tc.wantRule, "violations: %v", validationErr)
		})
	}
}

// Builds without an affected deployment are still valid events.
func TestBuildEventWithoutDeployments(t *testing.T) {
	e := event(t, notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED)
	e.GetBuildFailed().GetBuild().DeploymentIds = nil
	require.NoError(t, protovalidate.Validate(e))
}

func declaredEventTypes() []notificationsv1.EventType {
	types := make([]notificationsv1.EventType, 0, len(notificationsv1.EventType_name))
	for n := range notificationsv1.EventType_name {
		if n != 0 {
			types = append(types, notificationsv1.EventType(n))
		}
	}
	return types
}

func ruleIDs(err *protovalidate.ValidationError) []string {
	ids := make([]string, 0, len(err.Violations))
	for _, v := range err.Violations {
		ids = append(ids, v.Proto.GetRuleId())
	}
	return ids
}
