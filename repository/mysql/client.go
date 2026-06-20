package mysql

import (
	"cx3/config"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitMySQL(cfg *config.MySQLConfig) error {
	dsn := cfg.DSN()

	var logLevel logger.LogLevel
	logLevel = logger.Warn

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().Local()
		},
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	}

	db, err := gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		return fmt.Errorf("mysql open failed: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql db failed: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.ConnMaxIdleTime) * time.Second)

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("mysql ping failed: %w", err)
	}

	DB = db
	return nil
}

func CloseMySQL() {
	if DB != nil {
		sqlDB, _ := DB.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
}
