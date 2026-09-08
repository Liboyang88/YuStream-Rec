package main

import "github.com/spf13/viper"

// Env 只声明本脚本用到的字段，key 与 dcpro 的 .env 完全一致（mapstructure tag 相同）。
// 复用 dcpro 的 .env 文件，保证连的是同一套 MySQL / Milvus / embedding 服务。
type Env struct {
	MysqlDsn              string `mapstructure:"MYSQL_DSN"`
	MilvusCollectionName  string `mapstructure:"MILVUS_COLLECTION_NAME"`
	VecDim                int    `mapstructure:"VEC_DIM"`
	MilvusAddress         string `mapstructure:"MILVUS_ADDRESS"`
	EmbeddingModelApiKey  string `mapstructure:"EMBEDDING_MODEL_API_KEY"`
	EmbeddingModelBaseURL string `mapstructure:"EMBEDDING_MODEL_BASE_URL_WITH_OPENAI"`
}

func LoadEnv(path string) (*Env, error) {
	env := &Env{}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(env); err != nil {
		return nil, err
	}
	return env, nil
}
