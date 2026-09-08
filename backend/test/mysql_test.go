package main

import (
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"testing"
)

func TestMysqlConnection(t *testing.T) {
	// TODO: test mysql connection
	db, err := gorm.Open(mysql.Open("new_user:20060525@tcp(localhost:3306)/dcpro?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	fmt.Println(db, err)
	fmt.Println("mysql connection test")
}