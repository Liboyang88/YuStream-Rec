package domain


type Env struct {
	ServerAddress string `mapstructure:"SERVER_ADDRESS"`
	MysqlDsn      string `mapstructure:"MYSQL_DSN"`
	AccessTokenSecret      string `mapstructure:"ACCESS_TOKEN_SECRET"`
	RefreshTokenSecret     string `mapstructure:"REFRESH_TOKEN_SECRET"`
	AccessTokenExpire      int    `mapstructure:"ACCESS_TOKEN_EXPIRE_HOUR"`
	RefreshTokenExpire      int    `mapstructure:"REFRESH_TOKEN_EXPIRE_HOUR"`
	RedisAddress            string `mapstructure:"REDIS_ADDRESS"`
	RedisPassword            string `mapstructure:"REDIS_PASSWORD"`
	RedisDB                  int    `mapstructure:"REDIS_DB"`
	RedisExpireTime          int    `mapstructure:"REDIS_EXPIRE_TIME"`
	RedisIAExpireTime          int    `mapstructure:"REDIS_IA_EXPIRE_TIME"`
	RedisMaxLen                int    `mapstructure:"REDIS_MAX_LEN"`
	MilvusCollectionName     string `mapstructure:"MILVUS_COLLECTION_NAME"`
	VecDim             int    `mapstructure:"VEC_DIM"`
	MilvusAddress            string `mapstructure:"MILVUS_ADDRESS"`
	SearchTopK                 int    `mapstructure:"SEARCH_TOP_K"`
	LimitTopK                  int    `mapstructure:"LIMIT_TOP_K"`
	RecsWindow                 int    `mapstructure:"RECS_WINDOW"`
	ModelName 				string `mapstructure:"MODEL_NAME"`
	ModelApiKey 			string `mapstructure:"MODEL_API_KEY"`
	EmbeddingModelName         string `mapstructure:"EMBEDDING_MODEL_NAME"`
	EmbeddingModelApiKey     string `mapstructure:"EMBEDDING_MODEL_API_KEY"`
	EmbeddingModelBaseURL    string `mapstructure:"EMBEDDING_MODEL_BASE_URL_WITH_OPENAI"`
}