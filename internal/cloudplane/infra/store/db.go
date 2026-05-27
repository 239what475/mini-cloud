package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	commonid "mini-cloud/internal/common/id"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Store 聚合 cloud-plane 的数据库读写方法。
type Store struct {
	// db 提供持久化状态访问。
	db *sql.DB
}

// Open 打开 cloud-plane PostgreSQL 数据库连接并校验驱动配置。
// 参数说明：databaseURL 是 PostgreSQL 连接串。
func Open(databaseURL string) (*sql.DB, error) {
	// sql.Open 只创建连接池对象，不会立即验证数据库可达性。
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 用短超时 ping 一次数据库，避免启动时长时间卡在不可达连接上。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping 失败时主动关闭连接池，避免调用方拿到不可用的 db。
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(fmt.Errorf("ping database: %w", err), fmt.Errorf("close database after ping failure: %w", closeErr))
		}
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// 返回已经完成一次连通性检查的连接池。
	return db, nil
}

// New 构造持有数据库连接池的 Store。
// 参数说明：db 表示数据库连接。
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// newID 生成带业务前缀的短随机 ID。
// 参数说明：prefix 是 ID 的可读前缀。
func newID(prefix string) (string, error) {
	return commonid.New(prefix)
}
