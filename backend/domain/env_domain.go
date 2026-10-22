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
	// User Name->ID Cache 存活时间 【小时为单位】
	UserNameTTL             int    `mapstructure:"USER_NAME_TTL"`
	// User ID->Info Cache 存活时间 【小时为单位】
	UserInfoTTL             int    `mapstructure:"USER_INFO_TTL"`
	// Item ID->Item Cache 存活时间 【小时为单位】
	ItemTTL             int    `mapstructure:"ITEM_TTL"`
	// Interaction Cache 存活时间 【小时为单位】
	InteractionTTL             int    `mapstructure:"INTERACTION_TTL"`
	// Recommend Cache 存活时间 【分钟为单位】
	RecommendTTL             int    `mapstructure:"RECOMMEND_TTL"`
	// Interaction Record (以最近时间排序) 最大长度 【条】
	InteractionRecordMaxLen                int    `mapstructure:"INTERACTION_RECORD_MAX_LEN"`
	QueueUniqueTTL1            int    `mapstructure:"QUEUE_UNIQUE_TTL1"`
	QueueUniqueTTL2            int    `mapstructure:"QUEUE_UNIQUE_TTL2"`
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

	MysqlMaxIdleConns int `mapstructure:"MYSQL_MAX_IDLE_CONNS"`
	MysqlMaxOpenConns int `mapstructure:"MYSQL_MAX_OPEN_CONNS"`
	// 连接最大生命周期 【分钟为单位】
	MysqlConnMaxLifetime int `mapstructure:"MYSQL_CONN_MAX_LIFETIME"`
	// 连接最大空闲时间 【分钟为单位】
	MysqlConnMaxIdleTime int `mapstructure:"MYSQL_CONN_MAX_IDLE_TIME"`

	AliyunAccessKeyId string `mapstructure:"ALIYUN_ACCESS_KEY_ID"`
	AliyunAccessKeySecret string `mapstructure:"ALIYUN_ACCESS_KEY_SECRET"`
	AliyunEmail string `mapstructure:"ALIYUN_EMAIL"`
	AliyunFrom string `mapstructure:"ALIYUN_FROM"`
	MailAccountName string `mapstructure:"MAIL_ACCOUNT_NAME"`

	LogLevel string `mapstructure:"LOG_LEVEL"`
}