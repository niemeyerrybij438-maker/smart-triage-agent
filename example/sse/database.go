package main

import (
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	schemaGorm "gorm.io/gorm/schema"
)

func NewMysqlGrom(source string, logLevel logger.LogLevel) (*gorm.DB, error) {
	if !strings.Contains(source, "parseTime") {
		source += "?charset=utf8mb4&parseTime=True&loc=Local"
	}
	gdb, err := gorm.Open(mysql.Open(source), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schemaGorm.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, err
	}

	// 閰嶇疆GORM鏃ュ織
	var gormLogger logger.Interface
	if logLevel > 0 {
		gormLogger = logger.Default.LogMode(logLevel)
	} else {
		gormLogger = logger.Default.LogMode(logger.Silent)
	}

	gdb.Logger = gormLogger

	return gdb, nil
}
