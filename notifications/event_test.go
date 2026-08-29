// Copyright 2021-2026 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package notifications_test

import (
	"encoding/json"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationsv1 "github.com/cerbos/cloud-api/genpb/cerbos/cloud/notifications/v1"
)

func buildFailedEvent() *notificationsv1.Event {
	return &notificationsv1.Event{
		Id:           "X9K2M4P7Q1R3S5T8",
		Type:         notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
		OccurredAt:   timestamppb.Now(),
		WorkspaceId:  "B6C0NNZO5VO6",
		DeploymentId: new("D4F8H2J6L0N3"),
		Link:         "https://hub.cerbos.cloud/w/B6C0NNZO5VO6/builds/X9K2M4P7Q1R3S5T8",
		Payload: &notificationsv1.Event_BuildFailed{
			BuildFailed: &notificationsv1.BuildFailed{
				Build: &notificationsv1.Build{
					Id:       "X9K2M4P7Q1R3S5T8",
					StoreIds: []string{"S1T2U3V4W5X6"},
				},
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
		},
	}
}

// The webhook body documented in notifications.proto is protojson of the Event:
// the type as its enum name and the payload under a key named after the type.
func TestEventWebhookJSON(t *testing.T) {
	event := buildFailedEvent()
	require.NoError(t, protovalidate.Validate(event), "fixture must be a valid event")

	body, err := protojson.Marshal(event)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "EVENT_TYPE_BUILD_FAILED", decoded["type"])
	require.Contains(t, decoded, "buildFailed")

	var roundTripped notificationsv1.Event
	require.NoError(t, protojson.Unmarshal(body, &roundTripped))
	require.Empty(t, cmp.Diff(event, &roundTripped, protocmp.Transform()))
}

func TestEventValidation(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*notificationsv1.Event)
		wantRule string
	}{
		{
			name:     "workspace id of the wrong shape",
			mutate:   func(e *notificationsv1.Event) { e.WorkspaceId = "b6c0nnzo5vo6" },
			wantRule: "string.pattern",
		},
		{
			name:     "type names a different payload",
			mutate:   func(e *notificationsv1.Event) { e.Type = notificationsv1.EventType_EVENT_TYPE_BUILD_SUCCEEDED },
			wantRule: "event.type_matches_payload",
		},
		{
			name:     "deployment-scoped event without a deployment",
			mutate:   func(e *notificationsv1.Event) { e.DeploymentId = nil },
			wantRule: "event.deployment_scoped",
		},
		{
			name: "test failure in which nothing failed",
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
			event := buildFailedEvent()
			tc.mutate(event)

			err := protovalidate.Validate(event)
			require.Error(t, err)

			var validationErr *protovalidate.ValidationError
			require.ErrorAs(t, err, &validationErr)
			require.Contains(t, ruleIDs(validationErr), tc.wantRule, "violations: %v", validationErr)
		})
	}
}

func ruleIDs(err *protovalidate.ValidationError) []string {
	ids := make([]string, 0, len(err.Violations))
	for _, v := range err.Violations {
		ids = append(ids, v.Proto.GetRuleId())
	}
	return ids
}
