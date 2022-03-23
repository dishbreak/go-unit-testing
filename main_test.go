package main

import (
	"context"
	"errors"
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
	st := NewFakeSnapshotTaker()
	bm := &BackupManager{
		st:     st,
		prefix: "testing",
	}
	err := bm.TriggerSnapshots("my-cluster-1", "my-cluster-2", "my-cluster-3")
	assert.Nil(t, err)
	assert.Equal(t, []snapshotCreationRecord{
		{"my-cluster-1", "testing-my-cluster-1"},
		{"my-cluster-2", "testing-my-cluster-2"},
		{"my-cluster-3", "testing-my-cluster-3"},
	}, st.journal)
}

func TestTriggerSnapshotsWithContinueError(t *testing.T) {
	// this snapshot taker will fail to create a snapshot for my-cluster-2
	st := NewFlakySnapshotTaker("my-cluster-2", &types.DBClusterNotFoundFault{})
	bm := &BackupManager{
		st:     st,
		prefix: "testing",
	}
	err := bm.TriggerSnapshots("my-cluster-1", "my-cluster-2", "my-cluster-3")
	assert.Nil(t, err)
	assert.Equal(t, []snapshotCreationRecord{
		{"my-cluster-1", "testing-my-cluster-1"},
		{"my-cluster-3", "testing-my-cluster-3"},
	}, st.journal)
}

func TestTriggerSnapshotsWithError(t *testing.T) {
	// this snapshot taker will fail to create a snapshot for my-cluster-2
	// the generic error will cause BackupManager to return early with the error.
	st := NewFlakySnapshotTaker("my-cluster-2", errors.New("general failure"))
	bm := &BackupManager{
		st:     st,
		prefix: "testing",
	}
	err := bm.TriggerSnapshots("my-cluster-1", "my-cluster-2", "my-cluster-3")
	assert.NotNil(t, err)
	assert.Equal(t, []snapshotCreationRecord{
		{"my-cluster-1", "testing-my-cluster-1"},
	}, st.journal)
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
