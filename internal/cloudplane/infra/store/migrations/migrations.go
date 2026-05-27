package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var files embed.FS

// Up 执行 cloud-plane 当前开发期 schema baseline。
// 说明：cloud-plane 尚未正式发布，历史开发期迁移已压缩为单一 baseline；
// 已有旧本地数据库需要重建，不支持从旧 goose version 原地升级。
// 参数说明：db 表示数据库连接。
func Up(db *sql.DB) error {
	// goose 从内嵌 FS 读取 baseline SQL 文件。
	goose.SetBaseFS(files)
	// 当前 store 只支持 PostgreSQL。
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	// 执行 baseline 迁移到最新版本。
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run goose up: %w", err)
	}
	return nil
}
