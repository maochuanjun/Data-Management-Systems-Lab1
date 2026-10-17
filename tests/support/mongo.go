// Package support contains fixtures shared by the external test suites.
package support

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"questionnaire/internal/store"
)

// Mongo owns exactly one random database. The URI's default database is never
// used, and cleanup only drops the database allocated by this fixture.
type Mongo struct {
	Context context.Context
	Store   *store.Store
	DB      *mongo.Database
}

func NewMongo(t *testing.T) *Mongo {
	t.Helper()
	uri := os.Getenv("INTEGRATION_MONGO_URI")
	if uri == "" {
		t.Skip("set INTEGRATION_MONGO_URI to run real MongoDB integration tests")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	database := "questionnaire_test_" + hex.EncodeToString(random[:])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	raw, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	// 先检查数据库可用性，避免每个集成用例等待驱动长时间超时。
	// 连接失败时不注册删除测试库的清理任务，以免再次超时。
	pingCtx, stopPing := context.WithTimeout(context.Background(), 5*time.Second)
	pingErr := raw.Ping(pingCtx, nil)
	stopPing()
	if pingErr != nil {
		closeCtx, stopClose := context.WithTimeout(context.Background(), 2*time.Second)
		_ = raw.Disconnect(closeCtx)
		stopClose()
		t.Fatalf("MongoDB 测试连接失败：%v。默认本地数据库请先执行 make mongo-up；自定义数据库请检查 INTEGRATION_MONGO_URI", pingErr)
	}
	db := raw.Database(database)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if err := db.Drop(cleanup); err != nil {
			t.Errorf("drop exact test DB %s: %v", database, err)
		}
		if err := raw.Disconnect(cleanup); err != nil {
			t.Errorf("disconnect inspection client: %v", err)
		}
	})
	repo, err := store.New(ctx, uri, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := repo.Close(cleanup); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})
	return &Mongo{Context: ctx, Store: repo, DB: db}
}
