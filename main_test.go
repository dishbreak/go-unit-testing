package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/stretchr/testify/assert"
)

type snapshotCreationRecord struct {
	DBClusterIdentifier         string
	DBClusterSnapshotIdentifier string
}

type fakeSnapshotTaker struct {
	journal []snapshotCreationRecord
}

func (f *fakeSnapshotTaker) CreateDBClusterSnapshot(ctx context.Context, in *rds.CreateDBClusterSnapshotInput, optFns ...func(*rds.Options)) (*rds.CreateDBClusterSnapshotOutput, error) {
	f.journal = append(f.journal, snapshotCreationRecord{*in.DBClusterIdentifier, *in.DBClusterSnapshotIdentifier})
	return &rds.CreateDBClusterSnapshotOutput{
		DBClusterSnapshot: &types.DBClusterSnapshot{
			DBClusterIdentifier:         in.DBClusterIdentifier,
			DBClusterSnapshotIdentifier: in.DBClusterSnapshotIdentifier,
		},
	}, nil
}

func (f *fakeSnapshotTaker) GetJournal() []snapshotCreationRecord {
	return f.journal
}

func NewFakeSnapshotTaker() *fakeSnapshotTaker {
	return &fakeSnapshotTaker{
		journal: make([]snapshotCreationRecord, 0),
	}
}

type flakySnapshotTaker struct {
	*fakeSnapshotTaker
	offensiveClusterID string
	err                error
}

func NewFlakySnapshotTaker(offensiveClusterID string, err error) *flakySnapshotTaker {
	return &flakySnapshotTaker{
		fakeSnapshotTaker:  NewFakeSnapshotTaker(),
		offensiveClusterID: offensiveClusterID,
		err:                err,
	}
}

func (f *flakySnapshotTaker) CreateDBClusterSnapshot(ctx context.Context, in *rds.CreateDBClusterSnapshotInput, optFns ...func(*rds.Options)) (*rds.CreateDBClusterSnapshotOutput, error) {
	if *in.DBClusterIdentifier == f.offensiveClusterID {
		return nil, f.err
	}
	return f.fakeSnapshotTaker.CreateDBClusterSnapshot(ctx, in, optFns...)
}

func TestTriggerSnapshots(t *testing.T) {
	type testCase struct {
		clusterIDs      []string
		st              SnapshotTaker
		expectedError   error
		expectedJournal []snapshotCreationRecord
	}

	unhandledError := &types.DBClusterSnapshotAlreadyExistsFault{}
	testCases := map[string]testCase{
		"happy path with no errors": {
			clusterIDs: []string{"my-cluster-1", "my-cluster-2", "my-cluster-3"},
			st:         NewFakeSnapshotTaker(),
			expectedJournal: []snapshotCreationRecord{
				{"my-cluster-1", "testing-my-cluster-1"},
				{"my-cluster-2", "testing-my-cluster-2"},
				{"my-cluster-3", "testing-my-cluster-3"},
			},
		},
		"encounters cluster not found error": {
			clusterIDs: []string{"my-cluster-1", "my-cluster-2", "my-cluster-3"},
			st:         NewFlakySnapshotTaker("my-cluster-2", &types.DBClusterNotFoundFault{}),
			expectedJournal: []snapshotCreationRecord{
				{"my-cluster-1", "testing-my-cluster-1"},
				{"my-cluster-3", "testing-my-cluster-3"},
			},
		},
		"encounters unexpected error": {
			clusterIDs:    []string{"my-cluster-1", "my-cluster-2", "my-cluster-3"},
			st:            NewFlakySnapshotTaker("my-cluster-2", unhandledError),
			expectedError: unhandledError,
			expectedJournal: []snapshotCreationRecord{
				{"my-cluster-1", "testing-my-cluster-1"},
			},
		},
		"no identifiers passed in": {
			st:              NewFakeSnapshotTaker(),
			expectedError:   ErrNoIdentifiersSpecified,
			expectedJournal: []snapshotCreationRecord{},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			bm := &BackupManager{
				st:     tc.st,
				prefix: "testing",
			}

			err := bm.TriggerSnapshots(tc.clusterIDs...)
			assert.ErrorIs(t, tc.expectedError, err)

			type journaler interface {
				GetJournal() []snapshotCreationRecord
			}

			j, ok := tc.st.(journaler)
			assert.True(t, ok, "cannot use SnapshotTaker as journaler")
			assert.Equal(t, tc.expectedJournal, j.GetJournal())
		})
	}
}

func TestFormSnapshotIdentifier(t *testing.T) {
	type testCase struct {
		input  string
		result string
	}

	testCases := map[string]testCase{
		"no truncation when less than 64 characters": {
			input:  "my-cluster-1",
			result: "testing-my-cluster-1",
		},
		"truncates down to 64 characters": {
			input:  "my-cluster-1-11111111111111111111111111111111111111111110",
			result: "testing-my-cluster-1-1111111111111111111111111111111111111111111",
		},
		"doesn't end with a hyphen": {
			input:  "my-cluster-1-",
			result: "testing-my-cluster-1",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// no need to set a SnapshotTaker for this test
			bm := &BackupManager{prefix: "testing"}
			assert.Equal(t, tc.result, bm.formSnapshotIdentifier(tc.input))
		})
	}
}
