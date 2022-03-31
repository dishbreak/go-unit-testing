# Evolution of Testing: Unit Testing Devops Code

## A Simple Backup Script

Let's say that you're using AWS Relational Database Service (RDS) and want to trigger snapshot backups against a bunch of RDS clusters. (Between you and me, this is really a good reason to use [AWS Backup](https://docs.aws.amazon.com/aws-backup/latest/devguide/whatisbackup.html), but let's keep on this example).

The following code would accept a bunch of cluster identifiers as arguments and do the needful.

```go
package main

import (
	"context"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		panic(err)
	}

	rdsClient := rds.NewFromConfig(cfg)
	// os.Args[0] will always be the name of the executable, so skip it.
	for _, name := range os.Args[1:] {
		snapshotName := strings.Join([]string{"backup", name}, "-")
		// truncate to 64 characters
		if len(snapshotName) => 64 {
			snapshotName = snapshotName[:64]
		}
		// remove the hyphen
		snapshotName = strings.TrimSuffix(snapshotName, "-")
		rdsClient.CreateDBClusterSnapshot(context.TODO(), &rds.CreateDBClusterSnapshotInput{
			DBClusterIdentifier: aws.String(name),
			DBClusterSnapshotIdentifier: aws.String(snapshotName),
		})
	}
}
```

However, when you submit your code for review, you get some stiff feedback. Namely, there's two issues here:

* The code isn't getting tested.
* There's no error handling at all for the `CreateDBClusterSnapshot()` call. 

Let's refactor and see if we can address this feedback.

## Use a Struct Intead of `main()`

One easy refactor here would be to place the code that creates the snapshots in its own struct. That would let us test the code in unit tests! Here's what the struct would look like. There's a few differences between this code and the previous example: 

* We follow the AWS SDK for Go V2 guidelines on [error handling](https://aws.github.io/aws-sdk-go-v2/docs/handling-errors/) and will log when a cluster isn't found but return an error immediately for any other (unexpected) errors.
* The `TriggerSnapshots()` function is a [variadic function](https://golangdocs.com/variadic-functions-in-golang) that can accept multiple cluster identifiers
* The code uses Dave Cheney's [Constant errors](https://dave.cheney.net/2016/04/07/constant-errors) pattern to return an error `ErrNoIdentifiersSpecified` if there's no cluster identifiers passed in
* We declare a prefix field and let the user specify a prefix instead of generating the timestamp inside the struct--the backup manager doesn't care about the prefix, we can let that be a concern of the caller

```go
type BackupManager struct {
	rdsClient *rds.Client
	prefix    string
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
```

And here's how the `main()` function would refactor. Note that we're using a `...` to unpack `os.Args[1:]` into variadic arguments for our `TriggerSnapshots()` method.

```go
	rdsClient := rds.NewFromConfig(cfg)
	bm := &BackupManager{
		rdsClient: rdsClient,
		prefix: fmt.Sprintf("run-%d", time.Now().Unix())
	}

	if err := bm.TriggerSnapshots(os.Args[1:]...); err != nil {
		panic(err)
	}
```

This code looks great, but there's a big problem when you go to write unit tests. Let's take another look. 

```go
type BackupManager struct {
	rdsClient *rds.Client //D'oh! Can't test this...
	prefix    string
}
```

Aw, shucks. The way this code is written, it can only work with an `*rds.Client` struct, meaning we have to use a real AWS client. This is going to make testing hard. We don't want to have to run commands against real AWS infrastructure to test! What do we do?

## Refactor to an Interface

A useful concept when writing testable code is [dependency injection](https://en.wikipedia.org/wiki/Dependency_injection). From Wikipedia:

> In software engineering, dependency injection is a technique in which an object receives other objects that it depends on, called dependencies. Typically, the receiving object is called a client and the passed-in ('injected') object is called a service.

Without realizing it, we've been using dependency injection already! We inject an RDS client into our `BackupManager` struct. However, we need a little more control over the dependency. It'd be nice to be able to swap out the dependency for our unit testing without modifiying the code under test.

Fortunately, Go has a useful feature--[implicit interfaces](https://go.dev/tour/methods/10). Put simply:

> A type implements an interface by implementing its methods. There is no explicit declaration of intent, no "implements" keyword.

This is huge. A package supplying a type doesn't need to declare that it implements an interface. Given an interface, the compiler will check a given type and see if it implements all the specified methods. If it does, it's an implementation of the interface, simple as that.

This can be a little hard to grok without an example, so let's provide one. If we define a `SnapshotTaker` interface like so, we can change our `BackupManager` to use it instead.

```go
type SnapshotTaker interface {
	CreateDBClusterSnapshot(context.Context, *rds.CreateDBClusterSnapshotInput, ...func(*rds.Options)) (*rds.CreateDBClusterSnapshotOutput, error)
}

type BackupManager struct {
	st SnapshotTaker
}
```

Note that this doesn't change much in our main function at all! Because `*rds.Client` implements the interface, even though the AWS SDK has no concept of my `SnapshotTaker` interface, I can use the RDS Client in my `BackupManager`.

```go
	rdsClient := rds.NewFromConfig(cfg)
	bm := &BackupManager{
		st: rdsClient,
    ...
	}
```

Coming from a language like Java this can be a really strange concept to understand. In Java, in order to use an object as an implementation of an interface, its class (or one of its parent classes) must _state_ that it implements the interface. In Go, implicit interfaces make things much simpler, as we'll see in a second.

Now that we've implemented an interface, it's time to unit test!

## Unit Testing with Interfaces

When unit testing, we want to assert the following:

* The code makes calls to the AWS API as expected, and returns correctly when there are no errors.
* If a given RDS cluster isn't found, the code will continue on.
* If an unhandled error occurs, the code will stop execution.

Let's focus first on the path with no errors--the "happy path", if you will. Unlike other languages, [Go has a built-in test runner and framework](https://gobyexample.com/testing), so we can get a unit test up and running in a jiffy. First, we'll need an implementation of our `SnapshotTaker` interface. Meet `fakeSnapshotTaker`.

```go
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
```

There's two key features of this mock implementation. First, it implements our `SnapshotTaker` interface, providing a `CreateDBClusterSnapshot()` method with the right signature. Second, it journals all calls against its method. This second feature will make it easy to check if we called the API correctly!

This is one of the huge benefits of using interfaces for our dependencies. The RDS client implements [dozens](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/rds#Client) of methods, but all we care about is one. Small interfaces are easier to mock and test with.

Here's what a unit test with this mock interface looks like.

```go
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
```

We're so close to getting this done. But we still need error handling!

## Error Testing with A Flaky Service

Our BackupManager still has an untested code path.

```go
_, err := b.st.CreateDBClusterSnapshot(...)
if err != nil {
	var cnfErr *types.DBClusterNotFoundFault
	if errors.As(err, &cnfErr) {
		log.Printf("Not backing up '%s', cluster not found.", clusterIdentifer)
		continue
	}
	return err
}
```

It'd be nice if our mock implementation could throw an error. That'd let us exercise our two different error handling scenarios. Let's meet our `flakySnapshotTaker`. 

```go
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
```

This `flakySnapshotTaker` looks a lot like our `fakeSnapshotTaker`. Indeed, that's largely because of two things:

1. It implements the `SnapshotTaker` interface. 
2. It [embeds](https://eli.thegreenplace.net/2020/embedding-in-go-part-1-structs-in-structs/) the `fakeSnapshotTaker`, meaning it overlays on top of our existing struct.

The big difference is that we can instruct the `flakySnapshotTaker` to return an error based on a specific input. This lets us test our error handling like so:

```go
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
```

Note that the `flakySnapshotTaker` does not pass on the call to create the snapshot for `my-cluster-2`, so when we check the journal, we don't see a call for a snapshot on `my-cluster-2`. We can make a similar test case for a more generic error. This would prompt the code to return early, meaning the resulting error would be non-nil and the journal would show only 1 snapshot created.

```go
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
```

Awesome. We've now taken care of error handling!

## Bonus Topic: Table-driven Tests

This code is way more modular and it's now got some tests that prove out its error handling. However, a reviewer notes that there's an issue with your following code:

```go
		snapshotName := strings.Join([]string{b.prefix, clusterIdentifer}, "-")
		// truncate to 64 characters
		if len(snapshotName) >= 64 {
			snapshotName = snapshotName[:64]
		}
		// remove the hyphen
		snapshotName = strings.TrimSuffix(snapshotName, "-")
```

None of the tests we've written exercise this code at all! We could write more tests to handle this, but it's going to get tedious really fast...

```go
func TestClusterAndPrefixGreaterThan64Chars(t *testing.T) {...}
func TestClusterAndPrefixLessThan64Chars(t *testing.T) {...}
func TestClusterAndPrefixWithoutTrailingHyphen(t *testing.T) {...}
```

There'd be a lot of overlap in these functions. It seems like a great opportunity to embrace Dave Cheney's [table-driven tests pattern](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests).

First, let's refactor this code into its own function. It'll make it easier to test it in isolation.

```go
func (b *BackupManager) formSnapshotIdentifier(clusterIdentifer string) (snapshotID string) {
	snapshotID = strings.Join([]string{b.prefix, clusterIdentifer}, "-")
	// truncate to 64 characters
	if len(snapshotID) >= 64 {
		snapshotID = snapshotID[:64]
	}
	// remove the hyphen
	snapshotID = strings.TrimSuffix(snapshotID, "-")
	return
}
```

Next, let's think about what a single test case will look like. 

```go
type testCase struct {
  input  string
  result string
}
```

The function has a single input and a single output. We rely on a single field from the `BackupManager` in the function, so that can be shared across runs. Now, we can build a table!

```go
testCases := map[string]testCase{
  "no truncation when less than 64 characters": {
    input: "my-cluster-1",
    result: "testing-my-cluster-1",
  },
  "truncates down to 64 characters": {
    input: "my-cluster-1-11111111111111111111111111111111111111111110",
    result: "testing-my-cluster-1-1111111111111111111111111111111111111111111",
  },
  "doesn't end with a hyphen": {
    input: "my-cluster-1-",
    result: "testing-my-cluster-1",
  },
}
```

Finally, we can use `Run()` inside the test to create sub-tests. 

```go
for name, tc := range testCases {
  t.Run(name, func(t *testing.T) {
    // no need to set a SnapshotTaker for this test
    bm := &BackupManager{prefix: "testing"}
    assert.Equal(t, tc.result, bm.formSnapshotIdentifier(tc.input))
  })
}
```

Say, now that we look at it, this pattern can work great for our `TriggerSnapshots()` testing too! We'll just need to adjust our `fakeSnapshotTaker` to add a method to retrieve the journal. Why this is required will be apparent in a moment.

```go
func (f *fakeSnapshotTaker) GetJournal() []snapshotCreationRecord {
	return f.journal
}
```

The test case struct for `TriggerSnapshots` is going to be a little bit larger. In addition to the cluster IDs, we'll need to include a `SnapshotTaker` implementation, an expected error, and an expected journal.

```go
type testCase struct {
  clusterIDs      []string
  st              SnapshotTaker
  expectedError   error
  expectedJournal []snapshotCreationRecord
}
```

We can now set up a test table to cover all the cases from before, plus an additional one to check the case where we accidentally providde no identifiers at all!

```go
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
```

Now, for the test runner. This is where things get a little spicy.

```go
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
```

We'll use the inputs from the test case to create a `BackupManager`, then feed it the cluster IDs from the test case. We'll then use a [type assertion](https://go.dev/tour/methods/15) to convert the snapshot taker into a `journaler` implementation. Note that because the `fakeSnapshotTaker` implements the `GetJournal()`method and the `flakySnapshotTaker` embeds `fakeSnapshotTaker`, both structs implement `GetJournal()` and should pass the type assertion. If the assertion fails, it means we should fail the test--we set it up poorly.

## Wrap-up

It took a journey to get here, but we have finally tightened this code down. Here's a reminder of what we've done today:

* Refactored code to make it more testable, lifting it out of `main()` and putting it in structs
* Using interfaces to make dependency injection and unit testing easier
* Embedding structs to create mock implmentations that let us test error handling
* Using table-driven tests that reduce boilerplate and make tests easier to read