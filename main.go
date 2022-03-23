package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// BackupManager
type BackupManager struct {
	st     SnapshotTaker
	prefix string
}

type SnapshotTaker interface {
	CreateDBClusterSnapshot(context.Context, *rds.CreateDBClusterSnapshotInput, ...func(*rds.Options)) (*rds.CreateDBClusterSnapshotOutput, error)
}

type BackupManagerError string

func (b BackupManagerError) Error() string {
	return string(b)
}

const ErrNoIdentifiersSpecified BackupManagerError = "recieved no cluster identifiers"

func (b *BackupManager) TriggerSnapshots(clusterIdentifers ...string) error {
	if len(clusterIdentifers) == 0 {
		return ErrNoIdentifiersSpecified
	}

	for _, clusterIdentifer := range clusterIdentifers {
		snapshotName := strings.Join([]string{b.prefix, clusterIdentifer}, "-")
		// truncate to 64 characters
		if len(snapshotName) >= 64 {
			snapshotName = snapshotName[:64]
		}
		// remove the hyphen
		snapshotName = strings.TrimSuffix(snapshotName, "-")
		_, err := b.st.CreateDBClusterSnapshot(
			context.TODO(),
			&rds.CreateDBClusterSnapshotInput{
				DBClusterIdentifier:         aws.String(clusterIdentifer),
				DBClusterSnapshotIdentifier: aws.String(snapshotName),
			},
		)
		if err != nil {
			var cnfErr *types.DBClusterNotFoundFault
			if errors.As(err, &cnfErr) {
				log.Printf("Not backing up '%s', cluster not found.", clusterIdentifer)
				continue
			}
			return err
		}
	}
	return nil
}

func main() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		panic(err)
	}

	rdsClient := rds.NewFromConfig(cfg)
	bm := &BackupManager{
		st:     rdsClient,
		prefix: fmt.Sprintf("run-%d", time.Now().Unix()),
	}

	if err := bm.TriggerSnapshots(os.Args[1:]...); err != nil {
		panic(err)
	}
}
