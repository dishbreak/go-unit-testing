package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// BackupManager
type BackupManager struct {
	rdsClient *rds.Client
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
		_, err := b.rdsClient.CreateDBClusterSnapshot(
			context.TODO(),
			&rds.CreateDBClusterSnapshotInput{
				DBClusterIdentifier: aws.String(clusterIdentifer),
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
		rdsClient: rdsClient,
	}

	if err := bm.TriggerSnapshots(os.Args[1:]...); err != nil {
		panic(err)
	}
}
