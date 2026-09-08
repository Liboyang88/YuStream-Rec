package main

import (
	"github.com/gin-gonic/gin"
)

func WebBegin() {
	server := gin.Default()
	server.GET("/hello", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, World!",
		})
	})
	server.Run(":8081")
}

