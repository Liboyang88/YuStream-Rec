package main

import (
	"context"

	"fmt"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
)

func TestMilvusConnection(t *testing.T) {
	cxt := context.Background()
	_, err := client.NewClient(cxt, client.Config{
		Address: "localhost:19530",
	})
	
	if err != nil {
		t.Errorf("Milvus connection failed: %v", err)
	}

	fmt.Println("milvus connection test")
}